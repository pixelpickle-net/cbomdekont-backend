package fscache

import (
	"errors"
	"github.com/fsnotify/fsnotify"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Watcher struct {
	dir       string
	fsWatcher *fsnotify.Watcher
	Cache     *sync.Map
}

func NewWatch(dir string) (*Watcher, error) {
	if len(dir) < 1 {
		return nil, errors.New("directory is empty")
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		dir:       dir,
		fsWatcher: fw,
		Cache:     new(sync.Map),
	}

	log.Printf("fscache start watcher for %s", w.dir)
	err = w.fsWatcher.Add(w.dir)
	if err != nil {
		return nil, err
	}
	err = w.updateCache()
	if err != nil {
		return nil, err
	}

	return w, nil
}

func (w *Watcher) Watch() {
	go func() {
		for {
			select {
			case event := <-w.fsWatcher.Events:
				if event.Op&fsnotify.Create == fsnotify.Create {
					if filepath.Base(event.Name) == "..data" {
						err := w.updateCache()
						if err != nil {
							log.Printf("fscache update error %v", err)
						} else {
							log.Printf("fscache reload %s", w.dir)
						}
					}
				}
			case err := <-w.fsWatcher.Errors:
				log.Printf("fswatcher %s error %v", w.dir, err)
			}
		}
	}()
}

func (w *Watcher) updateCache() error {
	fileMap := make(map[string]string)
	files, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}

	for _, file := range files {
		name := filepath.Base(file.Name())
		if !file.IsDir() && !strings.HasPrefix(name, ".") {
			b, err := os.ReadFile(filepath.Join(w.dir, file.Name()))
			if err != nil {
				return err
			}
			fileMap[name] = string(b)
		}
	}

	w.Cache.Range(func(key, value interface{}) bool {
		if _, ok := fileMap[key.(string)]; !ok {
			w.Cache.Delete(key)
		}
		return true
	})

	for k, v := range fileMap {
		w.Cache.Store(k, v)
	}

	return nil
}
