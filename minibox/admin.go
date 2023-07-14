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
	"net/http"

	"github.com/pangbox/server/admin"
	"github.com/rs/zerolog"
)

type AdminOptions struct {
	Logger zerolog.Logger
	Addr   string
}

type AdminServer struct {
	service *Service
}

func NewAdmin(ctx context.Context) *AdminServer {
	web := new(AdminServer)
	web.service = NewService(ctx)
	return web
}

func (w *AdminServer) Configure(opts AdminOptions) error {
	log := opts.Logger
	spawn := func(ctx context.Context, service *Service) {
		AdminServer := http.Server{Addr: opts.Addr, Handler: admin.New(admin.Options{
			Logger: opts.Logger,
		})}

		service.SetShutdownFunc(func(shutdownCtx context.Context) error {
			return AdminServer.Shutdown(shutdownCtx)
		})

		if ctx.Err() != nil {
			log.Error().Err(ctx.Err()).Msg("cancelled before admin server could start")
			return
		}

		err := AdminServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("error serving admin server")
		}
	}

	return w.service.Configure(spawn)
}

func (w *AdminServer) Running() bool {
	return w.service.Running()
}

func (w *AdminServer) Start() error {
	return w.service.Start()
}

func (w *AdminServer) Stop() error {
	return w.service.Stop()
}
