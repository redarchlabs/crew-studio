package connections

import (
	"context"
	"strings"
	"testing"

	"crewstudio/internal/model"
)

func TestKM2ConnectionsAreTestedByTheKM2Client(t *testing.T) {
	svc := &Service{timeout: defaultTestTimeout}
	res := svc.Test(context.Background(), model.Connection{Kind: model.KindKM2, Config: map[string]string{"base_url": "http://127.0.0.1:1"}})
	if res.Supported || res.OK || !strings.Contains(res.Message, "KM2 client") {
		t.Errorf("km2 = %+v", res)
	}
}
