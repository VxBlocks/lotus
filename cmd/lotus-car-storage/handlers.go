package main

import (
	"net/http"
	"sync"

	"contrib.go.opencensus.io/exporter/prometheus"
	"github.com/gorilla/mux"
)

type handler struct {
	CarDataPath []string `json:"-"`
	MachineName string   `json:"machine_name"`
	SavePathIdx int
	StoreStatus *status `json:"store_status"`
}

type status struct {
	Version     string            `json:"version"`
	SectorCount int               `json:"sector_count"`
	Saving      int               `json:"saving"`
	saveLk      sync.RWMutex      `json:"-"`
	SectorPath  map[string]string `json:"sector_path"`
}

func (s *status) get(k string) string {
	s.saveLk.Lock()
	defer s.saveLk.Unlock()

	val, ok := s.SectorPath[k]
	if !ok {
		return ""
	}

	return val
}

func (s *status) getAll() map[string]string {
	s.saveLk.Lock()
	defer s.saveLk.Unlock()

	sectorPath := make(map[string]string)
	for key, value := range s.SectorPath {
		sectorPath[key] = value
	}

	return sectorPath
}

func (s *status) incrSave() {
	s.saveLk.Lock()
	defer s.saveLk.Unlock()
	s.Saving++
}

func (s *status) decrSave() {
	s.saveLk.Lock()
	defer s.saveLk.Unlock()
	s.Saving--
}

func (s *status) getLen() int {
	s.saveLk.Lock()
	defer s.saveLk.Unlock()
	return len(s.SectorPath)
}

func (s *status) add(k, v string) {
	s.saveLk.Lock()
	defer s.saveLk.Unlock()
	s.SectorPath[k] = v
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { // /remote/
	router := mux.NewRouter()
	router.HandleFunc("/remote/carRange/{id}", h.remoteGetCarRange).Methods("GET")
	router.HandleFunc("/remote/carCheck/{id}", h.remoteCheckCar).Methods("GET")

	exporter, err := prometheus.NewExporter(prometheus.Options{
		Namespace: "storage",
	})
	if err != nil {
		log.Fatalf("could not create the prometheus stats exporter: %v", err)
	}

	router.HandleFunc("/remote/storage/metrics", exporter.ServeHTTP).Methods("GET")
	router.ServeHTTP(w, r)
}
