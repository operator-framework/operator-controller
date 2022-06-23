package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

type Git struct {
	client.Reader
	SecretNamespace string
}

func (r *Git) Unpack(ctx context.Context, bundle *rukpakv1alpha1.Bundle) (*Result, error) {
	if bundle.Spec.Source.Type != rukpakv1alpha1.SourceTypeGit {
		return nil, fmt.Errorf("bundle source type %q not supported", bundle.Spec.Source.Type)
	}
	if bundle.Spec.Source.Git == nil {
		return nil, fmt.Errorf("bundle source git configuration is unset")
	}
	gitsource := bundle.Spec.Source.Git
	if gitsource.Repository == "" {
		// This should never happen because the validation webhook rejects git bundles without repository
		return nil, errors.New("missing git source information: repository must be provided")
	}

	// Set options for clone
	progress := bytes.Buffer{}
	cloneOpts := git.CloneOptions{
		URL:             gitsource.Repository,
		Progress:        &progress,
		Tags:            git.NoTags,
		InsecureSkipTLS: bundle.Spec.Source.Git.Auth.InsecureSkipVerify,
	}

	if bundle.Spec.Source.Git.Auth.Secret.Name != "" {
		userName, password, err := r.getCredentials(ctx, bundle)
		if err != nil {
			return nil, err
		}
		cloneOpts.Auth = &http.BasicAuth{Username: userName, Password: password}
	}

	if gitsource.Ref.Branch != "" {
		cloneOpts.ReferenceName = plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", gitsource.Ref.Branch))
		cloneOpts.SingleBranch = true
		cloneOpts.Depth = 1
	} else if gitsource.Ref.Tag != "" {
		cloneOpts.ReferenceName = plumbing.ReferenceName(fmt.Sprintf("refs/tags/%s", gitsource.Ref.Tag))
		cloneOpts.SingleBranch = true
		cloneOpts.Depth = 1
	}

	// Clone
	repo, err := git.CloneContext(ctx, memory.NewStorage(), memfs.New(), &cloneOpts)
	if err != nil {
		return nil, fmt.Errorf("bundle unpack git clone error: %v - %s", err, progress.String())
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("bundle unpack error: %v", err)
	}

	// Checkout commit
	if gitsource.Ref.Commit != "" {
		commitHash := plumbing.NewHash(gitsource.Ref.Commit)
		if err := wt.Reset(&git.ResetOptions{
			Commit: commitHash,
			Mode:   git.HardReset,
		}); err != nil {
			return nil, fmt.Errorf("checkout commit %q: %v", commitHash.String(), err)
		}
	}

	var bundleFS fs.FS = &billyFS{wt.Filesystem}

	// Subdirectory
	if gitsource.Directory != "" {
		directory := filepath.Clean(gitsource.Directory)
		if directory[:3] == "../" || directory[0] == '/' {
			return nil, fmt.Errorf("get subdirectory %q for repository %q: %s", gitsource.Directory, gitsource.Repository, "directory can not start with '../' or '/'")
		}
		sub, err := wt.Filesystem.Chroot(filepath.Clean(directory))
		if err != nil {
			return nil, fmt.Errorf("get subdirectory %q for repository %q: %v", gitsource.Directory, gitsource.Repository, err)
		}
		bundleFS = &billyFS{sub}
	}

	resolvedSource := &rukpakv1alpha1.BundleSource{
		Type: rukpakv1alpha1.SourceTypeGit,
		// TODO: improve git source implementation to return result with commit hash.
		Git: bundle.Spec.Source.Git.DeepCopy(),
	}

	return &Result{Bundle: bundleFS, ResolvedSource: resolvedSource, State: StateUnpacked}, nil
}

// getCredentials reads credentials from the secret specified in the bundle
// It returns the username ane password when they are in the secret
func (r *Git) getCredentials(ctx context.Context, bundle *rukpakv1alpha1.Bundle) (string, string, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: r.SecretNamespace, Name: bundle.Spec.Source.Git.Auth.Secret.Name}, secret)
	if err != nil {
		return "", "", err
	}
	userName := string(secret.Data["username"])
	password := string(secret.Data["password"])

	return userName, password, nil
}

// billy.Filesysten -> fs.FS
var (
	_ fs.FS         = &billyFS{}
	_ fs.ReadDirFS  = &billyFS{}
	_ fs.ReadFileFS = &billyFS{}
	_ fs.StatFS     = &billyFS{}
	_ fs.File       = &billyFile{}
)

type billyFS struct {
	billy.Filesystem
}

func (f *billyFS) ReadFile(name string) ([]byte, error) {
	file, err := f.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(file)
}

func (f *billyFS) Open(path string) (fs.File, error) {
	file, err := f.Filesystem.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Filesystem.Stat(path)
	return &billyFile{file, fi, err}, nil
}

func (f *billyFS) ReadDir(name string) ([]fs.DirEntry, error) {
	fis, err := f.Filesystem.ReadDir(name)
	if err != nil {
		return nil, err
	}
	entries := make([]fs.DirEntry, 0, len(fis))
	for _, fi := range fis {
		entries = append(entries, fs.FileInfoToDirEntry(fi))
	}
	return entries, nil
}

type billyFile struct {
	billy.File
	fi    os.FileInfo
	fiErr error
}

func (b billyFile) Stat() (fs.FileInfo, error) {
	return b.fi, b.fiErr
}
