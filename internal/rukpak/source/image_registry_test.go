package source_test

import (
	"context"
	"testing"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/operator-framework/operator-controller/internal/rukpak/source"
)

func BenchmarkUnpack(b *testing.B) {
	b.StopTimer()
	cacheDir := b.TempDir()
	mgr, _ := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{})
	unpacker, _ := source.NewDefaultUnpacker(mgr, "default", cacheDir)

	logger := zap.New(
		zap.UseFlagOptions(
			&zap.Options{
				Development: true,
			},
		),
	)

	ctx := log.IntoContext(context.Background(), logger)

	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			Ref: "quay.io/eochieng/litmus-edge-operator-bundle@sha256:104b294fa1f4c2e45aa0eac32437a51de32dce0eff923eced44a0dddcb7f363f",
		},
	}
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		_, _ = unpacker.Unpack(ctx, bundleSource)
	}
}
