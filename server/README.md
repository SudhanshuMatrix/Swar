# Swar server

The Go backend for Swar: Native Music Metadata Engine inspired by OMMR (Deezer, Apple iTunes, MusicBrainz, Spotify), user-triggered Deep Search (yt-dlp), playback descriptors, queue, explainable recommendations, radio, library, offline-download bookkeeping and Swar Jam (listen together over WebSockets).

```bash
make run        # start on :8765 (data in ./data/swar.db)
make check      # vet + race tests + build
make docker     # container image
```

Federated multi-provider search is active by default across zero-key providers (Deezer, iTunes, MusicBrainz); read [docs/DECISIONS.md](docs/DECISIONS.md) for architectural details.

- [API reference](docs/API.md)
- [Design decisions](docs/DECISIONS.md)
- [Deployment and sizing](docs/DEPLOYMENT.md)
- [Migration from the old backend](docs/MIGRATION.md)
- [Status report](STATUS.md)
- Configuration: [.env.example](.env.example)
