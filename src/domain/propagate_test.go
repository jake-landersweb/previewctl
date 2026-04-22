package domain

import (
	"reflect"
	"sort"
	"testing"
)

// airgoods-style config: many services share apps/backend, a subset reference
// provisioner.postgres.*, and web references {{proxy.url.web}} only. Postgres
// reset should affect everything under apps/backend plus nothing else.
func airgoodsLikeConfig() *ProjectConfig {
	return &ProjectConfig{
		Services: map[string]ServiceConfig{
			"backend": {
				Path: "apps/backend",
				Env: map[string]string{
					"DATABASE_URL": "{{provisioner.postgres.CONNECTION_STRING}}",
					"DB_HOST":      "{{provisioner.postgres.DB_HOST}}",
				},
				Start: "node build/src/index.js",
			},
			"queue": {
				Path:  "apps/backend",
				Env:   map[string]string{"QUEUE_PORT": "{{self.port}}"},
				Start: "node build/src/queue.js",
			},
			"user-events": {
				Path:  "apps/backend",
				Env:   map[string]string{"USER_EVENT_PORT": "{{self.port}}"},
				Start: "node build/src/user-events.js",
			},
			"websocket": {
				Path:  "apps/backend",
				Env:   map[string]string{"WEBSOCKET_PORT": "{{self.port}}"},
				Start: "node build/src/websocket.js",
			},
			"web": {
				Path:  "apps/web",
				Env:   map[string]string{"VITE_APP_API_URL": "{{services.backend.port}}"},
				Start: "npx vite preview",
			},
		},
	}
}

func TestServicesAffectedByChange_ProvisionerRefsExpandViaSharedPath(t *testing.T) {
	cfg := airgoodsLikeConfig()
	needsCompose, affected := servicesAffectedByChange(cfg, "postgres", nil)
	if needsCompose {
		t.Errorf("no Start references postgres — needsCompose should be false")
	}
	want := []string{"backend", "queue", "user-events", "websocket"}
	got := keys(affected)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("affected services = %v, want %v", got, want)
	}
	if affected["web"] {
		t.Errorf("web does not share apps/backend and should not be affected")
	}
}

func TestServicesAffectedByChange_NoReferencesMeansEmptySet(t *testing.T) {
	cfg := airgoodsLikeConfig()
	_, affected := servicesAffectedByChange(cfg, "nonexistent", nil)
	if len(affected) != 0 {
		t.Errorf("unknown provisioner should affect nothing, got %v", keys(affected))
	}
}

func TestServicesAffectedByChange_StartRefForcesCompose(t *testing.T) {
	cfg := &ProjectConfig{
		Services: map[string]ServiceConfig{
			"svc": {
				Path:  "apps/svc",
				Start: "run --db={{provisioner.postgres.CONNECTION_STRING}}",
			},
		},
	}
	needsCompose, affected := servicesAffectedByChange(cfg, "postgres", nil)
	if !needsCompose {
		t.Errorf("Start reference should force compose regen")
	}
	if !affected["svc"] {
		t.Errorf("svc should be affected")
	}
}

func TestServicesAffectedByChange_StoreKeyReferences(t *testing.T) {
	cfg := &ProjectConfig{
		Services: map[string]ServiceConfig{
			"gateway": {
				Path: "apps/gateway",
				Env:  map[string]string{"API_KEY": "{{store.TURBO_TOKEN}}"},
			},
			"worker": {
				Path: "apps/worker",
				Env:  map[string]string{"PORT": "{{self.port}}"},
			},
		},
	}
	_, affected := servicesAffectedByChange(cfg, "", []string{"TURBO_TOKEN"})
	if !affected["gateway"] {
		t.Errorf("gateway references {{store.TURBO_TOKEN}}, should be affected")
	}
	if affected["worker"] {
		t.Errorf("worker does not reference store, should not be affected")
	}
}

func TestDiffStoreKeys(t *testing.T) {
	cases := []struct {
		name          string
		before, after map[string]string
		want          []string
	}{
		{
			name:   "no change",
			before: map[string]string{"A": "1", "B": "2"},
			after:  map[string]string{"A": "1", "B": "2"},
			want:   nil,
		},
		{
			name:   "value change",
			before: map[string]string{"A": "1"},
			after:  map[string]string{"A": "2"},
			want:   []string{"A"},
		},
		{
			name:   "added key",
			before: map[string]string{},
			after:  map[string]string{"NEW": "x"},
			want:   []string{"NEW"},
		},
		{
			name:   "removed key",
			before: map[string]string{"OLD": "x"},
			after:  map[string]string{},
			want:   []string{"OLD"},
		},
		{
			name:   "mixed",
			before: map[string]string{"A": "1", "B": "2", "REMOVED": "x"},
			after:  map[string]string{"A": "1", "B": "changed", "NEW": "y"},
			want:   []string{"B", "NEW", "REMOVED"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diffStoreKeys(tc.before, tc.after)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("diffStoreKeys = %v, want %v", got, tc.want)
			}
		})
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
