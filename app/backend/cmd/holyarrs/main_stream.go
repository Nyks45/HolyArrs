package main

import (
	"context"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	"github.com/HolyArrs/holyarrs/app/backend/internal/stream"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
)

func newStreamer(
	s *database.Store,
	vault *secrets.Vault,
	configDir string,
	listenAddr func() string,
	subs *subtitles.Service,
	trakt *metadata.TraktAuth,
	md metadata.Provider,
) *stream.Manager {
	m := stream.NewManager(s, vault, configDir, listenAddr)
	m.Subtitles = subs
	m.SetExtras(vault, trakt)
	if md != nil {
		m.ResolveIMDb = func(ctx context.Context, kind, tmdbID string) string {
			if tmdbID == "" {
				return ""
			}
			cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if kind == "show" {
				if sp, ok := md.(metadata.ShowProvider); ok {
					if sh, err := sp.Show(cctx, "tmdb:"+tmdbID); err == nil {
						return sh.IDs["imdb"]
					}
				}
				return ""
			}
			if mv, err := md.Movie(cctx, "tmdb:"+tmdbID); err == nil {
				return mv.IDs["imdb"]
			}
			return ""
		}
	}
	return m
}

func startStreamPresets(ctx context.Context, m *stream.Manager) func() {
	presetCtx, stopPresets := context.WithCancel(ctx)
	presetsDone := make(chan struct{})
	go func() {
		defer close(presetsDone)
		m.BootstrapDefaultsIfEmpty(presetCtx)
	}()
	return func() {
		stopPresets()
		<-presetsDone
		m.Close()
	}
}
