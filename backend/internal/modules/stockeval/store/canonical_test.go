package store

import "testing"

func TestCanonicalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"clés triées, espaces retirés", `{ "b": 1, "a": [2.50, "x"] }`, `{"a":[2.50,"x"],"b":1}`, false},
		{"pas d'échappement HTML", `{"a":"<&>"}`, `{"a":"<&>"}`, false},
		{"reste après la valeur", `{} {}`, "", true},
		{"invalide", `{`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := canonicalJSON([]byte(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestInputsHashStable(t *testing.T) {
	t.Parallel()
	a, err := InputsHash([]byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := InputsHash([]byte("{\n \"b\": 2, \"a\": 1}"))
	if a != b || len(a) != 64 {
		t.Fatalf("empreintes %s / %s", a, b)
	}
}

func TestActorDefault(t *testing.T) {
	t.Parallel()
	if got := actorFrom(t.Context()); got != "system" {
		t.Fatalf("acteur par défaut = %q", got)
	}
}
