package catalog

import (
	"strings"
	"testing"
)

func TestBuildBundle_WithCRDAndDeployment(t *testing.T) {
	b, err := buildBundle("abc123", "test-abc123", "1.0.0", []BundleOption{WithCRD(), WithDeployment(), WithConfigMap()})
	if err != nil {
		t.Fatal(err)
	}

	if b.version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", b.version)
	}

	// Check CRD has parameterized names
	crd, ok := b.files["manifests/crd.yaml"]
	if !ok {
		t.Fatal("expected manifests/crd.yaml")
	}
	if !strings.Contains(string(crd), "e2e-abc123.e2e.operatorframework.io") {
		t.Error("CRD group should contain scenario ID")
	}
	if !strings.Contains(string(crd), "e2e-abc123tests") {
		t.Error("CRD plural should contain scenario ID")
	}

	// Check CSV references parameterized deployment name
	csv, ok := b.files["manifests/csv.yaml"]
	if !ok {
		t.Fatal("expected manifests/csv.yaml")
	}
	if !strings.Contains(string(csv), "test-operator-abc123") {
		t.Error("CSV deployment name should contain scenario ID")
	}
	if !strings.Contains(string(csv), "busybox:1.36") {
		t.Error("CSV should use busybox:1.36 as container image")
	}

	// Check script ConfigMap
	scriptCM, ok := b.files["manifests/script-configmap.yaml"]
	if !ok {
		t.Fatal("expected manifests/script-configmap.yaml")
	}
	if !strings.Contains(string(scriptCM), "httpd-script-abc123") {
		t.Error("script ConfigMap name should contain scenario ID")
	}

	// Check test ConfigMap
	cm, ok := b.files["manifests/bundle-configmap.yaml"]
	if !ok {
		t.Fatal("expected manifests/bundle-configmap.yaml")
	}
	if !strings.Contains(string(cm), "test-configmap-abc123") {
		t.Error("test ConfigMap name should contain scenario ID")
	}

	// Check metadata/annotations.yaml exists and contains package name
	ann, ok := b.files["metadata/annotations.yaml"]
	if !ok {
		t.Fatal("expected metadata/annotations.yaml")
	}
	if !strings.Contains(string(ann), "test-abc123") {
		t.Error("annotations should contain package name")
	}

	// Check NetworkPolicy
	np, ok := b.files["manifests/networkpolicy.yaml"]
	if !ok {
		t.Fatal("expected manifests/networkpolicy.yaml")
	}
	if !strings.Contains(string(np), "test-operator-abc123-network-policy") {
		t.Error("NetworkPolicy name should contain scenario ID")
	}
}

func TestBuildBundle_BadImage(t *testing.T) {
	b, err := buildBundle("bad1", "test-bad1", "1.0.2", []BundleOption{BadImage()})
	if err != nil {
		t.Fatal(err)
	}

	// BadImage bundles should have CSV with wrong/image
	csv, ok := b.files["manifests/csv.yaml"]
	if !ok {
		t.Fatal("BadImage bundle should have CSV")
	}
	if !strings.Contains(string(csv), "wrong/image") {
		t.Error("BadImage CSV should reference wrong/image")
	}

	// BadImage should also set hasCRD=true
	if _, ok := b.files["manifests/crd.yaml"]; !ok {
		t.Error("BadImage bundle should have CRD")
	}
}

func TestBuildBundle_NoCRD(t *testing.T) {
	b, err := buildBundle("nocrd", "test-nocrd", "1.0.0", []BundleOption{WithDeployment()})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.files["manifests/crd.yaml"]; ok {
		t.Error("should not have CRD when WithCRD not specified")
	}
	if _, ok := b.files["manifests/csv.yaml"]; !ok {
		t.Error("should have CSV when WithDeployment specified")
	}
}

func TestCatalog_FBCGeneration(t *testing.T) {
	cat := NewCatalog("test", "sc1",
		WithPackage("test",
			Bundle("1.0.0", WithCRD(), WithDeployment(), WithConfigMap()),
			Bundle("1.2.0", WithCRD(), WithDeployment()),
			Channel("alpha", Entry("1.0.0")),
			Channel("beta", Entry("1.0.0"), Entry("1.2.0", Replaces("1.0.0"))),
		),
	)

	if len(cat.packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(cat.packages))
	}
	pkg := cat.packages[0]
	if pkg.name != "test" {
		t.Errorf("expected package name 'test', got %q", pkg.name)
	}
	if len(pkg.bundles) != 2 {
		t.Errorf("expected 2 bundles, got %d", len(pkg.bundles))
	}
	if len(pkg.channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(pkg.channels))
	}

	// Verify channels by name rather than by index so the test is robust to ordering changes.
	var alpha, beta *channelDef
	for i := range pkg.channels {
		switch pkg.channels[i].name {
		case "alpha":
			alpha = &pkg.channels[i]
		case "beta":
			beta = &pkg.channels[i]
		}
	}
	if alpha == nil {
		t.Fatal("alpha channel not found")
	}
	if beta == nil {
		t.Fatal("beta channel not found")
	}

	// Verify alpha channel has 1 entry
	if len(alpha.entries) != 1 {
		t.Errorf("expected 1 alpha entry, got %d", len(alpha.entries))
	}

	// Verify beta channel has replaces edge
	if len(beta.entries) != 2 {
		t.Fatalf("expected 2 beta entries, got %d", len(beta.entries))
	}
	if beta.entries[1].replaces != "1.0.0" {
		t.Errorf("expected entry 1.2.0 to replace 1.0.0, got %q", beta.entries[1].replaces)
	}
}

func TestCatalog_MultiplePackages(t *testing.T) {
	cat := NewCatalog("test", "sc2",
		WithPackage("foo",
			Bundle("1.0.0", WithCRD(), WithDeployment()),
			Channel("stable", Entry("1.0.0")),
		),
		WithPackage("bar",
			Bundle("2.0.0", WithCRD(), WithDeployment()),
			Channel("stable", Entry("2.0.0")),
		),
	)

	if len(cat.packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(cat.packages))
	}
	if cat.packages[0].name != "foo" {
		t.Errorf("expected first package 'foo', got %q", cat.packages[0].name)
	}
	if cat.packages[1].name != "bar" {
		t.Errorf("expected second package 'bar', got %q", cat.packages[1].name)
	}
}
