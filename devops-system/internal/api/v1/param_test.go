package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humagin "github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"devops-system/internal/types"
)

func TestParamIDPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := humagin.New(r, huma.DefaultConfig("param-id-test", "v1"))
	var got types.ParamID
	huma.Register(api, huma.Operation{
		OperationID: "param-id",
		Method:      http.MethodGet,
		Path:        "/tenants/{id}",
	}, func(_ context.Context, in *struct {
		ID types.ParamID `path:"id"`
	}) (*struct{}, error) {
		got = in.ID
		return nil, nil
	})

	const want int64 = 367941961597845504
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tenants/367941961597845504", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s; want %d", rec.Code, rec.Body.String(), http.StatusNoContent)
	}
	if string(got) != "367941961597845504" {
		t.Fatalf("raw ID = %q", got)
	}
	parsed, err := got.Parse()
	if err != nil {
		t.Fatalf("parse ID: %v", err)
	}
	if int64(parsed) != want {
		t.Fatalf("parsed ID = %d, want %d", parsed, want)
	}
}
