package main

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Optum/dce/pkg/config"
	"github.com/Optum/dce/pkg/lease"
	"github.com/Optum/dce/pkg/lease/leaseiface/mocks"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

func TestUpdateLeaseByID(t *testing.T) {

	now := time.Now().Unix()
	type response struct {
		StatusCode int
		Body       string
	}
	tests := []struct {
		name        string
		expResp     response
		reqBody     string
		reqLease    *lease.Lease
		leaseID     string
		retLease    *lease.Lease
		retErr      error
		writeRetErr error
	}{
		{
			name:     "success",
			leaseID:  "1234567890",
			reqBody:  "{ \"budgetAmount\": 200.00 }",
			reqLease: &lease.Lease{},
			expResp: response{
				StatusCode: 200,
				Body: fmt.Sprintf("{\"budgetAmount\": 200.00,\"lastModifiedOn\":%d,\"createdOn\":%d }\n",
					now, now),
			},
			retLease: &lease.Lease{
				ID:             ptrString("1234567890"),
				BudgetAmount:   ptrFloat(200),
				CreatedOn:      &now,
				LastModifiedOn: &now,
			},
			retErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			r := httptest.NewRequest(
				"POST",
				fmt.Sprintf("http://example.com/leases/%s", tt.leaseID),
				strings.NewReader(fmt.Sprintf(tt.reqBody)),
			)

			r = mux.SetURLVars(r, map[string]string{
				"leaseId": tt.leaseID,
			})
			w := httptest.NewRecorder()

			cfgBldr := &config.ConfigurationBuilder{}
			svcBldr := &config.ServiceBuilder{Config: cfgBldr}

			leaseSvc := mocks.Servicer{}
			leaseSvc.On("Update", tt.leaseID, tt.reqLease).Return(
				tt.retLease, tt.retErr,
			)

			svcBldr.Config.WithService(&leaseSvc)
			_, err := svcBldr.Build()

			assert.Nil(t, err)
			if err == nil {
				Services = svcBldr
			}

			UpdateLeaseByID(w, r)

			resp := w.Result()
			body, err := io.ReadAll(resp.Body)

			assert.Nil(t, err)
			assert.Equal(t, tt.expResp.StatusCode, resp.StatusCode)
			assert.JSONEq(t, tt.expResp.Body, string(body))
		})
	}

}

func ptrFloat(s float64) *float64 {
	ptrS := s
	return &ptrS
}
