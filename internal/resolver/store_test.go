package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func storeDep(name string) string {
	return "[[dependency]]\nname = \"" + name + "\"\nsource = \"store\"\nversion = \"1\"\n"
}

func withSecret(manifest, secret string) string {
	return strings.Replace(manifest, `image = "x"`, "image = \"x\"\nsecrets = [\""+secret+"\"]", 1)
}

// fakeStore serves manifests by name, along with the metadata pointing at them.
func fakeStore(t *testing.T, manifests map[string]string) *httptest.Server {
	t.Helper()
	tempCache(t)
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	store := Store{Schema: StoreSchema, Packages: map[string]*StorePackage{}}
	for name, content := range manifests {
		sum := sha256.Sum256([]byte(content))
		store.Packages[name] = &StorePackage{Latest: "1", Versions: map[string]*StoreVersion{
			"1": {URL: server.URL + "/" + name + ".toml", SHA256: hex.EncodeToString(sum[:])},
		}}
		mux.HandleFunc("/"+name+".toml", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(content)) })
	}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/metadata.json", func(w http.ResponseWriter, r *http.Request) { w.Write(data) })
	t.Setenv("NFB_STORE", server.URL+"/metadata.json")
	return server
}

func TestStoreDependencyIsPinnedAndWorksOffline(t *testing.T) {
	server := fakeStore(t, map[string]string{"llvm": pkg("llvm", "")})
	root := write(t, t.TempDir()+"/package.nfb.toml", pkg("a", storeDep("llvm")))

	g, err := Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Lock.Store["llvm@1"] != server.URL+"/llvm.toml" {
		t.Errorf("lock: %v", g.Lock.Store)
	}
	if err := g.SaveLock(); err != nil {
		t.Fatal(err)
	}

	server.Close()
	g, err = Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if g.LockChanged() {
		t.Error("a repeat run changed the lock")
	}
}

func TestStoreRejectsWrongContent(t *testing.T) {
	cases := map[string]string{
		"different package": pkg("evil", ""),
		"different version": strings.Replace(pkg("llvm", ""), `version = "1"`, `version = "2"`, 1),
	}
	for name, content := range cases {
		fakeStore(t, map[string]string{"llvm": content})
		root := write(t, t.TempDir()+"/package.nfb.toml", pkg("a", storeDep("llvm")))
		if _, err := Resolve(context.Background(), root, Options{}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestStoreChecksumIsEnforced(t *testing.T) {
	tempCache(t)
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/llvm.toml", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(pkg("llvm", ""))) })
	mux.HandleFunc("/metadata.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Store{Schema: StoreSchema, Packages: map[string]*StorePackage{"llvm": {Versions: map[string]*StoreVersion{
			"1": {URL: server.URL + "/llvm.toml", SHA256: strings.Repeat("0", 64)},
		}}}})
	})
	t.Setenv("NFB_STORE", server.URL+"/metadata.json")

	root := write(t, t.TempDir()+"/package.nfb.toml", pkg("a", storeDep("llvm")))
	if _, err := Resolve(context.Background(), root, Options{}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("expected a checksum error, got %v", err)
	}
}

func TestStoreLookup(t *testing.T) {
	store := &Store{Packages: map[string]*StorePackage{
		"llvm": {Latest: "2", Versions: map[string]*StoreVersion{"1": {Yanked: true}, "2": {}}},
		"gone": {Versions: map[string]*StoreVersion{"1": {Yanked: true}}},
	}}
	if version, err := store.Lookup("llvm", ""); err != nil || version != "2" {
		t.Errorf("latest: %q, %v", version, err)
	}
	for _, c := range [][2]string{{"llvm", "1"}, {"llvm", "9"}, {"gone", ""}, {"nope", ""}} {
		if _, err := store.Lookup(c[0], c[1]); err == nil {
			t.Errorf("%v: expected an error", c)
		}
	}
}

func unitNamed(t *testing.T, g *Graph, name string) *Unit {
	t.Helper()
	for _, u := range g.Units {
		if u.Name() == name {
			return u
		}
	}
	t.Fatalf("no unit %s", name)
	return nil
}

func TestSecretsNeedApprovalAndStopAtDirectDependencies(t *testing.T) {
	fakeStore(t, map[string]string{
		"direct": withSecret(pkg("direct", storeDep("nested")), "TOKEN"),
		"nested": withSecret(pkg("nested", ""), "TOKEN"),
	})
	root := write(t, t.TempDir()+"/package.nfb.toml", withSecret(pkg("a", storeDep("direct")), "MINE"))

	g, err := Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if g.SecretAllowed(unitNamed(t, g, "direct"), "TOKEN") {
		t.Error("a fetched package got a secret without approval")
	}
	if !g.SecretAllowed(unitNamed(t, g, "a"), "MINE") {
		t.Error("your own manifest shouldn't need approval")
	}
	requests := g.SecretRequests()
	if len(requests) != 1 || requests[0].Package != "direct" {
		t.Fatalf("requests: %+v", requests)
	}

	g.ApproveSecret(requests[0])
	if !g.SecretAllowed(unitNamed(t, g, "direct"), "TOKEN") {
		t.Error("approval had no effect")
	}
	if g.SecretAllowed(unitNamed(t, g, "direct"), "OTHER") {
		t.Error("approval reached another secret")
	}
	if g.SecretAllowed(unitNamed(t, g, "nested"), "TOKEN") {
		t.Error("a nested package got a secret")
	}
	if err := g.SaveLock(); err != nil {
		t.Fatal(err)
	}

	for name, opts := range map[string]Options{"repeat": {}, "update": {Update: true}} {
		g, err := Resolve(context.Background(), root, opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.SecretRequests()) != 0 {
			t.Errorf("%s: forgot the approval", name)
		}
	}
}

func TestDirectAndNestedCountsAsDirect(t *testing.T) {
	fakeStore(t, map[string]string{
		"mid":  pkg("mid", storeDep("both")),
		"both": withSecret(pkg("both", ""), "TOKEN"),
	})
	root := write(t, t.TempDir()+"/package.nfb.toml", pkg("a", storeDep("mid")+storeDep("both")))

	g, err := Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := unitNamed(t, g, "both").Depth; got != 1 {
		t.Errorf("depth %d", got)
	}
}

func TestStoreDependencyCannotGoLocal(t *testing.T) {
	fakeStore(t, map[string]string{"evil": pkg("evil", local("../secret/package.nfb.toml"))})
	root := write(t, t.TempDir()+"/package.nfb.toml", pkg("a", storeDep("evil")))
	if _, err := Resolve(context.Background(), root, Options{}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected a rejection, got %v", err)
	}
}
