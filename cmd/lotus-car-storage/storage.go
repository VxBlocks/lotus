package main

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

var syncLock sync.Mutex

func (h *handler) remoteGetCarRange(w http.ResponseWriter, r *http.Request) {
	log.Infof("SERVE GET %s", r.URL)

	vars := mux.Vars(r)

	id := vars["id"]

	sp := h.StoreStatus.get(id)
	// The caller has a lock on this sector already, no need to get one here
	path := filepath.Join(sp, id)

	stat, err := os.Stat(path)
	if err != nil {
		log.Error("os stat err: %v", err)
		w.WriteHeader(500)
		return
	}

	if !stat.IsDir() {
		_, err = os.OpenFile(path, os.O_RDONLY, 0644) // nolint
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	// will do a ranged read over the file at the given path if the caller has asked for a ranged read in the request headers.
	http.ServeFile(w, r, path)
}

func (h *handler) remoteCheckCar(w http.ResponseWriter, r *http.Request) {
	log.Infof("SERVE GET %s", r.URL)

	vars := mux.Vars(r)

	id := vars["id"]

	sp := h.StoreStatus.get(id)
	// The caller has a lock on this sector already, no need to get one here
	path := filepath.Join(sp, id)

	stat, err := os.Stat(path)
	if err != nil {
		log.Error("os stat err: %v", err)
		w.WriteHeader(500)
		return
	}

	if stat.IsDir() || stat.Size() <= 0 {
		log.Error("os stat err: is not file")
		w.WriteHeader(500)
		return
	}

	if _, err = os.OpenFile(path, os.O_RDONLY, 0644); err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	lengthMap := map[string]int64{}
	lengthMap["length"] = stat.Size()

	b, err := json.Marshal(lengthMap)
	if err != nil {
		log.Errorf("json marshal err: %v", err)
	}

	w.WriteHeader(200)
	_, _ = w.Write(b)
	return
}
