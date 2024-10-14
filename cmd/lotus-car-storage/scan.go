package main

import (
	"golang.org/x/xerrors"
	"os"
	"strings"

	"path/filepath"
	"sync"
)

func (h *handler) scanPath() error {
	h.parseStoragePath(CAR_STORAGE_PATHS)
	storeCacheMap := make(map[string]string)
	for _, dataP := range h.CarDataPath {
		log.Infof("car dir: %s", dataP)
		sectorDirs, err := os.ReadDir(dataP)
		if err != nil {
			log.Errorf("car dir: %s %v", dataP, err)
			continue
		}
		for _, fileName := range sectorDirs {
			if !fileName.IsDir() {
				unsealed := filepath.Join(dataP, fileName.Name())
				err = assertFile(unsealed, uint64(0), uint64(55<<30))
				if err == nil {
					storeCacheMap[fileName.Name()] = dataP
				}
			}
		}
	}

	var syncMapLock sync.Mutex
	syncMapLock.Lock()
	for k, v := range storeCacheMap {
		h.StoreStatus.add(k, v)
	}
	syncMapLock.Unlock()

	log.Infof("scan total sector:%d", h.StoreStatus.getLen())
	return nil
}

func (h *handler) parseStoragePath(env string) {

	wEnv := os.Getenv(env)
	if wEnv == "" {
		return
	}

	h.CarDataPath = strings.Split(wEnv, ";")
	for _, root := range h.CarDataPath {
		dir, err := processDir(root)
		if err != nil {
			return
		}
		h.CarDataPath = append(h.CarDataPath, dir...)
	}
	h.CarDataPath = removeDuplicates(h.CarDataPath)
}

func processDir(root string) (dir []string, err error) {
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			dir = append(dir, path)
		}
		return nil
	})
	return dir, err
}

func removeDuplicates(strs []string) []string {
	if len(strs) <= 1 {
		return strs
	}

	seen := make(map[string]bool)
	result := make([]string, 0)

	for _, str := range strs {
		if _, ok := seen[str]; !ok {
			result = append(result, str)
			seen[str] = true
		}
	}

	return result
}

func assertFile(path string, minSz uint64, maxSz uint64) error {
	st2, err := os.Stat(path)
	if err != nil {
		return xerrors.Errorf("stat %s: %w", path, err)
	}

	if st2.IsDir() {
		return xerrors.Errorf("expected %s to be a regular file", path)
	}

	if uint64(st2.Size()) < minSz || uint64(st2.Size()) > maxSz {
		return xerrors.Errorf("%s wasn't within size bounds, expected %d < f < %d, got %d", path, minSz, maxSz, st2.Size())
	}

	return nil
}
