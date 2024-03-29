package api

import (
	"net/http"
	"strconv"
)

func (Server *RestAPIServer) chainHandler(w http.ResponseWriter, r *http.Request) {
	Server.l.Debug("Received chain REST API request")
	chainID, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch r.URL.Path {
	case "/startchain":
		err = Server.APIHandler.StartChain(r.Context(), chainID)
	case "/stopchain":
		err = Server.APIHandler.StopChain(r.Context(), chainID)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}
