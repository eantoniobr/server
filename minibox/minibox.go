// Copyright (C) 2018-2023, John Chadwick <john@jchw.io>
//
// Permission to use, copy, modify, and/or distribute this software for any purpose
// with or without fee is hereby granted, provided that the above copyright notice
// and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
// REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND
// FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
// INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS
// OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER
// TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF
// THIS SOFTWARE.
//
// SPDX-FileCopyrightText: Copyright (c) 2018-2023 John Chadwick
// SPDX-License-Identifier: ISC

package minibox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/pangbox/pangfiles/crypto/pyxtea"
	"github.com/pangbox/pangfiles/pak"
	"github.com/pangbox/server/common/hash"
	"github.com/pangbox/server/database"
	"github.com/pangbox/server/database/accounts"
	_ "github.com/pangbox/server/migrations"
	"github.com/pangbox/server/pangya/iff"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
	"github.com/xo/dburl"
	_ "modernc.org/sqlite"
)

type DataOptions struct {
	DatabaseURI string `json:"URI"`
}

type Options struct {
	WebAddr         string `json:"WebAddr"`
	AdminAddr       string `json:"AdminAddr"`
	QAAuthAddr      string `json:"QAAuthAddr"`
	LoginAddr       string `json:"LoginAddr"`
	GameAddr        string `json:"GameAddr"`
	MessageAddr     string `json:"MessageAddr"`
	ServerIP        string `json:"ServerIP"`
	GameServerName  string `json:"GameServerName"`
	GameChannelName string `json:"GameChannelName"`
	PangyaRegion    string `json:"PangyaRegion"`
	PangyaDir       string `json:"PangyaDir"`
	PangyaIFF       string `json:"PangyaIFF"`
}

type Server struct {
	mu  sync.RWMutex
	log zerolog.Logger // +checklocksignore

	// Fabric services
	// +checklocks:mu
	accountsService *accounts.Service

	// Network services
	// +checklocks:mu
	Topology *TopologyServer
	// +checklocks:mu
	Web *WebServer
	// +checklocks:mu
	Admin *AdminServer
	// +checklocks:mu
	Login *LoginServer
	// +checklocks:mu
	Game *GameServer
	// +checklocks:mu
	Message *MessageServer
	// +checklocks:mu
	QAAuth *QAAuthServer

	// Misc
	// +checklocks:mu
	Rugburn *RugburnPatcher

	// +checklocks:mu
	pangyaKey pyxtea.Key
	// +checklocks:mu
	pangyaFS *pak.FS
	// +checklocks:mu
	pangyaIFF *iff.Archive
	// +checklocks:mu
	lastDbOpts *DataOptions
	// +checklocks:mu
	lastOpts *Options
}

func dbConnectMigrate(log zerolog.Logger, urlstr string) (*sql.DB, error) {
	url, err := dburl.Parse(urlstr)
	if err != nil {
		return nil, fmt.Errorf("parsing database URL: %w", err)
	}

	db, err := database.OpenDBWithDriver(url.Driver, url.DSN)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	database.SetLogger(log)

	if err := goose.Up(db, "."); err != nil {
		return nil, fmt.Errorf("running database migrations: %w", err)
	}

	return db, nil
}

func NewServer(ctx context.Context, log zerolog.Logger) *Server {
	server := new(Server)
	server.log = log
	server.Topology = NewLocalTopology(ctx)
	server.Web = NewWeb(ctx)
	server.Admin = NewAdmin(ctx)
	server.Login = NewLoginServer(ctx)
	server.Game = NewGameServer(ctx)
	server.Message = NewMessageServer(ctx)
	server.QAAuth = NewQAAuth(ctx)
	server.Rugburn = NewRugburnPatcher()
	return server
}

// ConfigureDatabase configures the database, including running pending
// migrations. If the services are already configured, it will also re-run
// service configuration with the last configuration.
func (server *Server) ConfigureDatabase(opts DataOptions) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	// Do not do anything if the database settings are the same.
	if server.lastDbOpts != nil && server.lastDbOpts.DatabaseURI == opts.DatabaseURI {
		return nil
	}

	db, err := dbConnectMigrate(server.log, opts.DatabaseURI)
	if err != nil {
		return fmt.Errorf("setting up database: %w", err)
	}

	db.SetMaxOpenConns(1)

	server.accountsService = accounts.NewService(accounts.Options{
		Logger:   server.log,
		Database: db,
		Hasher:   hash.Bcrypt{},
	})

	// If the services are already running, reconfigure all of them.
	if server.lastOpts != nil {
		configureOpts := *server.lastOpts
		server.lastOpts = nil
		server.ConfigureServices(configureOpts)
	}

	server.lastDbOpts = &opts

	return nil
}

// ConfigureServices reconfigures services. You must successfully configure
// the database before running this.
func (server *Server) ConfigureServices(opts Options) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.accountsService == nil {
		return errors.New("database not configured yet")
	}

	if server.lastOpts.ShouldRedetectPangyaKey(opts) {
		key, err := getPakKey(server.log, opts.PangyaRegion, []string{
			filepath.Join(opts.PangyaDir, "projectg*.pak"),
			filepath.Join(opts.PangyaDir, "ProjectG*.pak"),
		})

		if err != nil {
			return fmt.Errorf("detecting pak region: %w", err)
		}

		server.pangyaFS, err = pak.LoadPaks(key, []string{filepath.Join(opts.PangyaDir, "*.pak")})
		if err != nil {
			return err
		}

		server.pangyaIFF, err = iff.LoadFromPak(server.log, *server.pangyaFS)
		if err != nil {
			return err
		}

		server.pangyaKey = key
	}

	if server.lastOpts.ShouldConfigureTopology(opts) {
		topologyOptions, err := server.topologyOptions(opts)
		if err != nil {
			return fmt.Errorf("configuring topology server: %w", err)
		}

		if err := server.Topology.Configure(topologyOptions); err != nil {
			return fmt.Errorf("configuring topology server: %w", err)
		}
	}

	if server.lastOpts.ShouldConfigureWeb(opts) {
		if err := server.Web.Configure(WebOptions{
			Logger:          server.log,
			Addr:            opts.WebAddr,
			PangyaKey:       server.pangyaKey,
			PangyaDir:       opts.PangyaDir,
			AccountsService: server.accountsService,
		}); err != nil {
			return fmt.Errorf("configuring web server: %w", err)
		}
	}

	if server.lastOpts.ShouldConfigureAdmin(opts) {
		if err := server.Admin.Configure(AdminOptions{
			Logger: server.log,
			Addr:   opts.AdminAddr,
		}); err != nil {
			return fmt.Errorf("configuring web server: %w", err)
		}
	}

	if server.lastOpts.ShouldConfigureQAAuth(opts) {
		if err := server.QAAuth.Configure(QAAuthOptions{
			Logger: server.log,
			Addr:   opts.QAAuthAddr,
		}); err != nil {
			return fmt.Errorf("configuring QA auth server: %w", err)
		}
	}

	if server.lastOpts.ShouldConfigureLogin(opts) {
		if err := server.Login.Configure(LoginOptions{
			Logger:          server.log,
			Addr:            opts.LoginAddr,
			TopologyClient:  server.Topology.Client(),
			AccountsService: server.accountsService,
		}); err != nil {
			return fmt.Errorf("configuring login server: %w", err)
		}
	}

	if server.lastOpts.ShouldConfigureGame(opts) {
		if err := server.Game.Configure(GameOptions{
			Logger:          server.log,
			Addr:            opts.GameAddr,
			TopologyClient:  server.Topology.Client(),
			AccountsService: server.accountsService,
			PangyaIFF:       server.pangyaIFF,
			ServerID:        20202,
			ChannelName:     opts.GameChannelName,
		}); err != nil {
			return fmt.Errorf("configuring game server: %w", err)
		}
	}

	if server.lastOpts.ShouldConfigureMessage(opts) {
		if err := server.Message.Configure(MessageOptions{
			Logger:          server.log,
			Addr:            opts.MessageAddr,
			TopologyClient:  server.Topology.Client(),
			AccountsService: server.accountsService,
		}); err != nil {
			return fmt.Errorf("configuring message server: %w", err)
		}
	}

	server.Rugburn.Configure(RugburnOptions{
		Logger:    server.log,
		PangyaDir: opts.PangyaDir,
	})

	server.lastOpts = &opts

	return nil
}

// ShouldRedetectPangyaKey returns true if the options changed require the
// PangYa key to be re-detected.
func (options *Options) ShouldRedetectPangyaKey(newOpts Options) bool {
	if options == nil {
		return true
	}
	return (options.PangyaDir != newOpts.PangyaDir ||
		options.PangyaRegion != newOpts.PangyaRegion ||
		options.PangyaIFF != newOpts.PangyaIFF)
}

// ShouldConfigureWeb returns true if the options changed require the webserver
// to be re-configured.
func (options *Options) ShouldConfigureWeb(newOpts Options) bool {
	if options == nil {
		return true
	}
	return (options.WebAddr != newOpts.WebAddr ||
		options.PangyaDir != newOpts.PangyaDir ||
		options.PangyaRegion != newOpts.PangyaRegion)
}

// ShouldConfigureAdmin returns true if the options changed require the admin
// server to be re-configured.
func (options *Options) ShouldConfigureAdmin(newOpts Options) bool {
	if options == nil {
		return true
	}
	return options.AdminAddr != newOpts.AdminAddr
}

// ShouldConfigureQAAuth returns true if the options changed require the QA
// auth server to be re-configured.
func (options *Options) ShouldConfigureQAAuth(newOpts Options) bool {
	if options == nil {
		return true
	}
	return options.QAAuthAddr != newOpts.QAAuthAddr
}

// ShouldConfigureLogin returns true if the options changed require the login
// server to be re-configured.
func (options *Options) ShouldConfigureLogin(newOpts Options) bool {
	if options == nil {
		return true
	}
	return options.LoginAddr != newOpts.LoginAddr
}

// ShouldConfigureGame returns true if the options changed require the game
// server to be re-configured.
func (options *Options) ShouldConfigureGame(newOpts Options) bool {
	if options == nil {
		return true
	}
	return (options.GameAddr != newOpts.GameAddr ||
		options.GameChannelName != newOpts.GameChannelName ||
		options.PangyaDir != newOpts.PangyaDir ||
		options.PangyaRegion != newOpts.PangyaRegion ||
		options.PangyaIFF != newOpts.PangyaIFF)
}

// ShouldConfigureMessage returns true if the options changed require the
// message server to be re-configured.
func (options *Options) ShouldConfigureMessage(newOpts Options) bool {
	if options == nil {
		return true
	}
	return options.MessageAddr != newOpts.MessageAddr
}

// ShouldConfigureMessage returns true if the options changed require the
// message server to be re-configured.
func (options *Options) ShouldConfigureTopology(newOpts Options) bool {
	if options == nil {
		return true
	}
	return (options.GameAddr != newOpts.GameAddr ||
		options.MessageAddr != newOpts.MessageAddr ||
		options.ServerIP != newOpts.ServerIP ||
		options.GameServerName != newOpts.GameServerName)
}

func (server *Server) topologyOptions(opts Options) (TopologyServerOptions, error) {
	gamePort, err := getPort(opts.GameAddr)
	if err != nil {
		return TopologyServerOptions{}, fmt.Errorf("failed to parse game server address: %s", opts.GameAddr)
	}

	messagePort, err := getPort(opts.MessageAddr)
	if err != nil {
		return TopologyServerOptions{}, fmt.Errorf("failed to parse message server address: %s", opts.GameAddr)
	}

	return TopologyServerOptions{
		Logger:         server.log,
		ServerIP:       opts.ServerIP,
		GameServerName: opts.GameServerName,
		GamePort:       gamePort,
		MessagePort:    messagePort,
	}, nil
}
