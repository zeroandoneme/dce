package main

import (
	"log"
	"net/http"

	"github.com/Optum/dce/pkg/api"
	"github.com/gorilla/mux"
)

// ResetAccount handles the HTTP request to reset an AWS account.
// It flags the account as NotReady and sends it to the reset queue.
func ResetAccount(w http.ResponseWriter, r *http.Request) {
	accountID := mux.Vars(r)["accountId"]

	// Log the incoming request
	log.Printf("Received reset request for Account ID: %s", accountID)

	// Flag account for reset (this updates DynamoDB and pushes to SQS queue)
	account, err := Services.AccountService().Reset(accountID)
	if err != nil {
		log.Printf("Failed to reset account %s: %v", *account.ID, err)
		api.WriteAPIErrorResponse(w, err)
		return
	}

	log.Printf("Account %s flagged for reset and queued successfully", *account.ID)

	// 204 No Content (no body)
	api.WriteAPIResponse(w, http.StatusNoContent, nil)
}
