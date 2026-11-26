package main

import (
	"context"
	"os"

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
) *stream.Manager {
	m := stream.NewManager(s, vault, configDir, listenAddr)
	m.Subtitles = subs
	m.SetExtras(vault, trakt)
	if configDir != "" && os.Getenv("HOLYARRS_CONFIG_IN_MEMORY") == "" {
		go m.BootstrapDefaultsIfEmpty(context.Background())
	}
	return m
}
