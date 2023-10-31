package main

import (
	"encoding/json"
	"net/http"

	"github.com/Optum/dce/pkg/api"
	"github.com/Optum/dce/pkg/errors"
	"github.com/Optum/dce/pkg/lease"
	validation "github.com/go-ozzo/ozzo-validation"
	"github.com/gorilla/mux"
)

// UpdateAccountByID updates an accounts information based on ID
func UpdateLeaseByID(w http.ResponseWriter, r *http.Request) {
	leaseID := mux.Vars(r)["leaseID"]

	// Deserialize the request JSON as an request object
	newLease := &lease.Lease{}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(newLease)
	if err != nil {
		api.WriteAPIErrorResponse(w,
			errors.NewBadRequest("invalid request parameters"))
		return
	}

	err = validation.ValidateStruct(newLease,
		// ID has to be empty
		validation.Field(&newLease.ID, validation.NilOrNotEmpty, validation.In(leaseID)),
	)
	if err != nil {
		api.WriteAPIErrorResponse(w,
			errors.NewValidation("lease", err))
		return
	}

	lease, err := Services.LeaseService().Update(leaseID, newLease)
	if err != nil {
		api.WriteAPIErrorResponse(w, err)
		return
	}

	api.WriteAPIResponse(w, http.StatusOK, lease)
}
