// Package webapp отдаёт собранный Mini App (Vite → dist). Файлы встраиваются в бинарь,
// чтобы образ был одним артефактом без отдельного веб-сервера.
package webapp

import (
	"embed"
	"fmt"
	"io/fs"
)

// В git лежит только dist/.gitkeep (Vite копирует его из public/), чтобы embed
// собирался в свежем клоне без сборки фронтенда.
//
//go:embed all:dist
var assets embed.FS

// Assets возвращает содержимое dist или ошибку, если фронтенд не собран.
func Assets() (fs.FS, error) {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return nil, fmt.Errorf("mini app is not built (npm run build in webapp): %w", err)
	}
	return dist, nil
}
