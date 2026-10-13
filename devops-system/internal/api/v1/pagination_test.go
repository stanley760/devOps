package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humagin "github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/stanley760/devOps/devops-common/domain"
	"github.com/stanley760/devOps/devops-infra/http/response"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

func TestRoleListPageParamEmbedding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := humagin.New(r, huma.DefaultConfig("pagination-test", "v1"))
	huma.Register(api, huma.Operation{
		OperationID: "role-list-pagination",
		Method:      http.MethodGet,
		Path:        "/roles",
	}, func(_ context.Context, in *RoleListInput) (*RoleListOutput, error) {
		return &RoleListOutput{
			Body: response.OKBody(domain.PageResult[types.RoleDTO]{
				List:     []types.RoleDTO{},
				PageNum:  in.PageNum,
				PageSize: in.PageSize,
			}),
		}, nil
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roles?page_num=3&page_size=25", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want %d", rec.Code, rec.Body.String(), http.StatusOK)
	}

	var body struct {
		Data domain.PageResult[types.RoleDTO] `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.PageNum != 3 || body.Data.PageSize != 25 {
		t.Fatalf("page metadata = (%d, %d), want (3, 25)", body.Data.PageNum, body.Data.PageSize)
	}
	if body.Data.List == nil || len(body.Data.List) != 0 {
		t.Fatalf("list = %#v, want empty non-nil list", body.Data.List)
	}
}
