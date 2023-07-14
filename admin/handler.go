// Copyright (C) 2023, John Chadwick <john@jchw.io>
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
// SPDX-FileCopyrightText: Copyright (c) 2023 John Chadwick
// SPDX-License-Identifier: ISC

package admin

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"
)

type Options struct {
	Logger zerolog.Logger
}

type Handler struct {
	router httprouter.Router
	log    zerolog.Logger
}

func New(opt Options) *Handler {
	return &Handler{
		router: *httprouter.New(),
		log:    opt.Logger,
	}
}

func (l *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.log.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("admin http request")
	l.router.ServeHTTP(w, r)
}
