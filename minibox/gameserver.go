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

	"github.com/pangbox/server/database/accounts"
	gameserver "github.com/pangbox/server/game/server"
	"github.com/pangbox/server/gameconfig"
	"github.com/pangbox/server/gen/proto/go/topologypb/topologypbconnect"
	"github.com/pangbox/server/pangya/iff"
	"github.com/rs/zerolog"
)

type GameOptions struct {
	Logger          zerolog.Logger
	Addr            string
	TopologyClient  topologypbconnect.TopologyServiceClient
	AccountsService *accounts.Service
	PangyaIFF       *iff.Archive
	ServerID        uint32
	ChannelName     string
}

type GameServer struct {
	service *Service
}

func NewGameServer(ctx context.Context) *GameServer {
	game := new(GameServer)
	game.service = NewService(ctx)
	return game
}

func (g *GameServer) Configure(opts GameOptions) error {
	log := opts.Logger
	spawn := func(ctx context.Context, service *Service) {
		gameServer := gameserver.New(gameserver.Options{
			Logger:          opts.Logger,
			TopologyClient:  opts.TopologyClient,
			AccountsService: opts.AccountsService,
			PangyaIFF:       opts.PangyaIFF,
			ServerID:        opts.ServerID,
			ChannelName:     opts.ChannelName,
			ConfigProvider:  gameconfig.Default(),
		})

		service.SetShutdownFunc(func(shutdownCtx context.Context) error {
			return gameServer.Shutdown(shutdownCtx)
		})

		if ctx.Err() != nil {
			log.Error().Err(ctx.Err()).Msg("cancelled before game server could start")
			return
		}

		err := gameServer.Listen(ctx, opts.Addr)
		if err != nil {
			log.Error().Err(err).Msg("error serving game server")
		}
	}

	return g.service.Configure(spawn)
}

func (g *GameServer) Running() bool {
	return g.service.Running()
}

func (g *GameServer) Start() error {
	return g.service.Start()
}

func (g *GameServer) Stop() error {
	return g.service.Stop()
}
