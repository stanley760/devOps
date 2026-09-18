package tenant

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareInjectsTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(true))
	r.GET("/", func(c *gin.Context) {
		id, err := RequiredTenantID(c.Request.Context())
		if err != nil {
			t.Fatal(err)
		}
		c.String(200, "%d", id)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(DefaultHeader, "7")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "7" {
		t.Fatalf("response = %d %q; want 200 7", rec.Code, rec.Body.String())
	}
}

func TestMiddlewareRejectsMissingTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(true))
	r.GET("/", func(c *gin.Context) { t.Fatal("handler should not run") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
