package operatore2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var (
	cfg              *rest.Config
	c                client.Client
	ctx              context.Context
	containerRuntime string
)

type BundleInfo struct {
	baseFolderPath string          // root path of the folder where the specific bundle type input data is stored
	bundles        []BundleContent // stores the data relevant to different versions of the bundle
}

type BundleContent struct {
	bInputDir     string // The directory that stores the specific version of bundle data
	bundleVersion string // The specific version of the bundle data
	imageRef      string // Stores the bundle image reference
}

type CatalogDInfo struct {
	baseFolderPath     string // root path to the folder storing the catalogs
	catalogDir         string // The folder storing the FBC template
	operatorName       string // Name of the operator to be installed from the bundles
	desiredChannelName string // Desired channel name for the operator
	imageRef           string // Stores the catalog image reference
	fbcFileName        string // Name of the FBC template file
}

type OperatorActionInfo struct {
	installVersion string // Version of the operator to be installed on the cluster
	upgradeVersion string // Version of the operator to be upgraded on the cluster
}

type SdkProjectInfo struct {
	projectName string // The operator-sdk project name
	domainName  string
	group       string
	version     string
	kind        string
}

const (
	remoteRegistryRepo     = "localhost:5001/"
	kindServer             = "operator-controller-op-dev-e2e"
	deployedNameSpace      = "rukpak-system"
	operatorControllerHome = "../.."
)

func TestOperatorFramework(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator Framework E2E Suite")
}

var _ = BeforeSuite(func() {

	var err error
	cfg = ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	err = catalogd.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	err = operatorv1alpha1.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	err = rukpakv1alpha1.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())

	ctx = context.Background()

	containerRuntime = os.Getenv("CONTAINER_RUNTIME") // This environment variable is set in the Makefile
	if containerRuntime == "" {
		containerRuntime = "docker"
	}

})

var _ = Describe("Operator Framework E2E for plain+v0 bundles", func() {
	var (
		sdkInfo         *SdkProjectInfo
		bundleInfo      *BundleInfo
		catalogDInfo    *CatalogDInfo
		operatorAction  *OperatorActionInfo
		operatorCatalog *catalogd.Catalog
		operator        *operatorv1alpha1.Operator
		err             error
	)
	BeforeEach(func() {
		sdkInfo = &SdkProjectInfo{
			projectName: "plain-example",
			domainName:  "plain.com",
			group:       "cache",
			version:     "v1alpha1",
			kind:        "Memcached1",
		}
		bundleInfo = &BundleInfo{
			baseFolderPath: "../../testdata/bundles/plain-v0",
			bundles: []BundleContent{
				{
					bundleVersion: "0.1.0",
				},
				{
					bundleVersion: "0.2.0",
				},
			},
		}
		catalogDInfo = &CatalogDInfo{
			baseFolderPath:     "../../testdata/catalogs",
			fbcFileName:        "catalog.yaml",
			operatorName:       "plain-operator",
			desiredChannelName: "beta",
		}
		operatorAction = &OperatorActionInfo{
			installVersion: "0.1.0",
			upgradeVersion: "0.2.0",
		}
		for i, b := range bundleInfo.bundles {
			bundleInfo.bundles[i].bInputDir = catalogDInfo.operatorName + ".v" + b.bundleVersion
			bundleInfo.bundles[i].imageRef = remoteRegistryRepo + catalogDInfo.operatorName + "-bundle:v" + b.bundleVersion
		}
		catalogDInfo.catalogDir = catalogDInfo.operatorName + "-catalog"
		catalogDInfo.imageRef = remoteRegistryRepo + catalogDInfo.catalogDir + ":test"
	})
	It("should succeed", func() {
		By("creating a new operator-sdk project")
		err = sdkInitialize(sdkInfo)
		Expect(err).ToNot(HaveOccurred())

		By("creating a new api and controller")
		err = sdkNewAPIAndController(sdkInfo)
		Expect(err).ToNot(HaveOccurred())

		By("generating CRD manifests")
		err = sdkGenerateManifests(sdkInfo)
		Expect(err).ToNot(HaveOccurred())

		By("generating bundle directory using kustomize")
		// Creates bundle structure for the given bundle versions
		// Bundle content is same for the different versions of bundle now
		for _, b := range bundleInfo.bundles {
			err = kustomizeGenPlainBundleDirectory(sdkInfo, bundleInfo.baseFolderPath, b)
			Expect(err).ToNot(HaveOccurred())
		}

		By("building/pushing/kind loading bundle images from bundle directories")
		for _, b := range bundleInfo.bundles {
			dockerContext := filepath.Join(bundleInfo.baseFolderPath, b.bInputDir)
			dockerfilePath := filepath.Join(dockerContext, "plainbundle.Dockerfile")
			err = buildPushLoadContainer(b.imageRef, dockerfilePath, dockerContext, kindServer, GinkgoWriter)
			Expect(err).ToNot(HaveOccurred())
		}

		By("generating catalog directory by forming FBC and dockerfile using custom function")
		imageRefsBundleVersions := make(map[string]string)
		for _, b := range bundleInfo.bundles {
			imageRefsBundleVersions[b.imageRef] = b.bundleVersion
		}
		err = genPlainCatalogDirectory(catalogDInfo, imageRefsBundleVersions) // the bundle image references and their respective versions are passed
		Expect(err).ToNot(HaveOccurred())

		By("building/pushing/kind loading the catalog images")
		dockerContext := catalogDInfo.baseFolderPath
		dockerfilePath := filepath.Join(dockerContext, fmt.Sprintf("%s.Dockerfile", catalogDInfo.catalogDir))
		err = buildPushLoadContainer(catalogDInfo.imageRef, dockerfilePath, dockerContext, kindServer, GinkgoWriter)
		Expect(err).ToNot(HaveOccurred())

		By("creating a Catalog CR and verifying the creation of respective packages and bundle metadata")
		bundleVersions := make([]string, len(bundleInfo.bundles))
		for i, bundle := range bundleInfo.bundles {
			bundleVersions[i] = bundle.bundleVersion
		}
		operatorCatalog, err = createCatalogCheckResources(operatorCatalog, catalogDInfo, bundleVersions)
		Expect(err).ToNot(HaveOccurred())

		By("creating an operator CR and verifying the operator operations")
		namespace := fmt.Sprintf("%s-system", sdkInfo.projectName)
		operator, err = createOperator(ctx, catalogDInfo.operatorName, operatorAction.installVersion)
		Expect(err).ToNot(HaveOccurred())
		checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.installVersion, bundleInfo.baseFolderPath, namespace)

		By("upgrading an operator and verifying the operator operations")
		operator, err = upgradeOperator(ctx, catalogDInfo.operatorName, operatorAction.upgradeVersion)
		Expect(err).ToNot(HaveOccurred())
		checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.upgradeVersion, bundleInfo.baseFolderPath, namespace)

		By("deleting the operator CR and verifying the operator doesn't exist after deletion")
		err = deleteOperator(ctx, catalogDInfo.operatorName)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			err = validateOperatorDeletion(catalogDInfo.operatorName)
			g.Expect(errors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())

		By("deleting the catalog CR and verifying the deletion")
		err = deleteCatalog(operatorCatalog)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			err = validateCatalogDeletion(operatorCatalog)
			g.Expect(errors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())
	})
	AfterEach(func() {
		// Clearing up data generated for the test
		var toDelete []string
		for _, b := range bundleInfo.bundles {
			toDelete = append(toDelete, filepath.Join(bundleInfo.baseFolderPath, b.bInputDir)) // delete the registry+v1 bundles formed
		}
		toDelete = append(toDelete, sdkInfo.projectName)                                                                               //delete the sdk project directory
		toDelete = append(toDelete, filepath.Join(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir))                               // delete the FBC formed
		toDelete = append(toDelete, filepath.Join(catalogDInfo.baseFolderPath, fmt.Sprintf("%s.Dockerfile", catalogDInfo.catalogDir))) // delete the catalog Dockerfile generated
		err = deleteFolderFile(toDelete)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("Operator Framework E2E for registry+v1 bundles", func() {
	var (
		sdkInfo                *SdkProjectInfo
		bundleInfo             *BundleInfo
		catalogDInfo           *CatalogDInfo
		operatorAction         *OperatorActionInfo
		operatorCatalog        *catalogd.Catalog
		operator               *operatorv1alpha1.Operator
		semverTemplateFileName string
		err                    error
	)
	BeforeEach(func() {
		sdkInfo = &SdkProjectInfo{
			projectName: "registry-operator",
			domainName:  "example2.com",
			group:       "cache",
			version:     "v1alpha1",
			kind:        "Memcached2",
		}
		bundleInfo = &BundleInfo{
			baseFolderPath: "../../testdata/bundles/registry-v1",
			bundles: []BundleContent{
				{
					bundleVersion: "0.1.0",
				},
				{
					bundleVersion: "0.2.0",
				},
			},
		}
		catalogDInfo = &CatalogDInfo{
			baseFolderPath: "../../testdata/catalogs",
			fbcFileName:    "catalog.yaml",
			operatorName:   "registry-operator",
		}
		operatorAction = &OperatorActionInfo{
			installVersion: "0.1.0",
			upgradeVersion: "0.2.0",
		}
		for i, b := range bundleInfo.bundles {
			bundleInfo.bundles[i].bInputDir = sdkInfo.projectName + ".v" + b.bundleVersion
			bundleInfo.bundles[i].imageRef = remoteRegistryRepo + sdkInfo.projectName + "-bundle:v" + b.bundleVersion
		}
		catalogDInfo.catalogDir = catalogDInfo.operatorName + "-catalog"
		catalogDInfo.imageRef = remoteRegistryRepo + catalogDInfo.catalogDir + ":test"

		semverTemplateFileName = "registry-semver.yaml"
	})
	It("should succeed", func() {
		By("creating a new operator-sdk project")
		err = sdkInitialize(sdkInfo)
		Expect(err).ToNot(HaveOccurred())

		By("creating new api and controller")
		err = sdkNewAPIAndController(sdkInfo)
		Expect(err).ToNot(HaveOccurred())

		By("generating CRD manifests")
		err = sdkGenerateManifests(sdkInfo)
		Expect(err).ToNot(HaveOccurred())

		By("generating the CSV")
		err = sdkGenerateCSV(sdkInfo)
		Expect(err).ToNot(HaveOccurred())

		By("generating bundle directory and building/pushing/kind loading bundle images from bundle directories")
		// Creates bundle structure for the specified bundle versions
		// Bundle content is same for the bundles now
		for _, b := range bundleInfo.bundles {
			err = sdkBundleComplete(sdkInfo, bundleInfo.baseFolderPath, b)
			Expect(err).ToNot(HaveOccurred())
		}

		By("generating catalog directory by forming FBC and dockerfile using opm tool, and validating the FBC formed")
		bundleImageRefs := make([]string, len(bundleInfo.bundles))
		for i, bundle := range bundleInfo.bundles {
			bundleImageRefs[i] = bundle.imageRef
		}
		err = genRegistryCatalogDirectory(catalogDInfo, bundleImageRefs, semverTemplateFileName)
		Expect(err).ToNot(HaveOccurred())

		By("building/pushing/kind loading the catalog images")
		dockerContext := catalogDInfo.baseFolderPath
		dockerFilePath := filepath.Join(dockerContext, fmt.Sprintf("%s.Dockerfile", catalogDInfo.catalogDir))
		err = buildPushLoadContainer(catalogDInfo.imageRef, dockerFilePath, dockerContext, kindServer, GinkgoWriter)
		Expect(err).ToNot(HaveOccurred())

		By("creating a Catalog CR and verifying the creation of respective packages and bundle metadata")
		bundleVersions := make([]string, len(bundleInfo.bundles))
		for i, bundle := range bundleInfo.bundles {
			bundleVersions[i] = bundle.bundleVersion
		}
		operatorCatalog, err = createCatalogCheckResources(operatorCatalog, catalogDInfo, bundleVersions)
		Expect(err).ToNot(HaveOccurred())

		By("creating an operator CR and verifying the operator operations")
		operator, err = createOperator(ctx, catalogDInfo.operatorName, operatorAction.installVersion)
		Expect(err).ToNot(HaveOccurred())
		checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.installVersion, bundleInfo.baseFolderPath, deployedNameSpace)

		By("upgrading an operator and verifying the operator operations")
		operator, err = upgradeOperator(ctx, catalogDInfo.operatorName, operatorAction.upgradeVersion)
		Expect(err).ToNot(HaveOccurred())
		checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.upgradeVersion, bundleInfo.baseFolderPath, deployedNameSpace)

		By("deleting the operator CR and verifying the operator doesn't exist")
		err = deleteOperator(ctx, catalogDInfo.operatorName)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			err = validateOperatorDeletion(catalogDInfo.operatorName)
			g.Expect(errors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())

		By("deleting the catalog CR and verifying the deletion")
		err = deleteCatalog(operatorCatalog)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func(g Gomega) {
			err = validateCatalogDeletion(operatorCatalog)
			g.Expect(errors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())
	})
	AfterEach(func() {
		var toDelete []string
		for _, b := range bundleInfo.bundles {
			toDelete = append(toDelete, filepath.Join(bundleInfo.baseFolderPath, b.bInputDir)) // delete the registry+v1 bundles formed
		}
		toDelete = append(toDelete, sdkInfo.projectName)                                                                               //delete the sdk project directory
		toDelete = append(toDelete, semverTemplateFileName)                                                                            // delete the semver template formed
		toDelete = append(toDelete, filepath.Join(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir))                               // delete the FBC formed
		toDelete = append(toDelete, filepath.Join(catalogDInfo.baseFolderPath, fmt.Sprintf("%s.Dockerfile", catalogDInfo.catalogDir))) // delete the catalog Dockerfile generated
		err = deleteFolderFile(toDelete)
		Expect(err).ToNot(HaveOccurred())
	})
})

// Creates a new operator-sdk project with the name sdkInfo.projectName.
// A project folder is created with the name sdkInfo.projectName and operator-sdk is initialized.
func sdkInitialize(sdkInfo *SdkProjectInfo) error {
	if err := os.Mkdir(sdkInfo.projectName, 0755); err != nil {
		return fmt.Errorf("Error creating the sdk project %v:%v", sdkInfo.projectName, err)
	}

	operatorSdkProjectAbsPath, _ := filepath.Abs(sdkInfo.projectName)
	operatorSdkProjectPath := "OPERATOR_SDK_PROJECT_PATH=" + operatorSdkProjectAbsPath
	operatorSdkArgs := "OPERATOR_SDK_ARGS= init --domain=" + sdkInfo.domainName
	cmd := exec.Command("make", "operator-sdk", operatorSdkProjectPath, operatorSdkArgs)
	cmd.Dir = operatorControllerHome
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error initializing the operator-sdk project %v: %v : %v", sdkInfo.projectName, string(output), err)
	}

	return nil
}

// Creates new API and controller for the given project with the name sdkInfo.projectName
func sdkNewAPIAndController(sdkInfo *SdkProjectInfo) error {
	operatorSdkProjectAbsPath, _ := filepath.Abs(sdkInfo.projectName)
	operatorSdkProjectPath := "OPERATOR_SDK_PROJECT_PATH=" + operatorSdkProjectAbsPath
	operatorSdkArgs := "OPERATOR_SDK_ARGS= create api --group=" + sdkInfo.group + " --version=" + sdkInfo.version + " --kind=" + sdkInfo.kind + " --resource --controller"
	cmd := exec.Command("make", "operator-sdk", operatorSdkProjectPath, operatorSdkArgs)
	cmd.Dir = operatorControllerHome
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error creating new API and controller for the operator-sdk project %v: %v : %v", sdkInfo.projectName, string(output), err)
	}

	// Checking if the API was created in the expected path
	apiFilePath := filepath.Join(sdkInfo.projectName, "api", sdkInfo.version, fmt.Sprintf("%s_types.go", strings.ToLower(sdkInfo.kind)))
	Expect(apiFilePath).To(BeAnExistingFile())

	// Checking if the controller was created in the expected path")
	controllerFilePath := filepath.Join(sdkInfo.projectName, "controllers", fmt.Sprintf("%s_controller.go", strings.ToLower(sdkInfo.kind)))
	Expect(controllerFilePath).To(BeAnExistingFile())

	return nil
}

// Updates the generated code if the API is changed.
// Generates and updates the CRD manifests
func sdkGenerateManifests(sdkInfo *SdkProjectInfo) error {
	// Update the generated code for the resources
	cmd := exec.Command("make", "generate")
	cmd.Dir = sdkInfo.projectName
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Error updating generated code for the operator-sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Generate and update the CRD manifests
	cmd = exec.Command("make", "manifests")
	cmd.Dir = sdkInfo.projectName
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Error generating and updating crd manifests for the operator-sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Checking if CRD manifests are generated in the expected path
	crdFilePath := filepath.Join(sdkInfo.projectName, "config", "crd", "bases", fmt.Sprintf("%s.%s_%ss.yaml", sdkInfo.group, sdkInfo.domainName, strings.ToLower(sdkInfo.kind)))
	Expect(crdFilePath).To(BeAnExistingFile())

	return nil
}

// Generates CSV for the bundle with default values
func sdkGenerateCSV(sdkInfo *SdkProjectInfo) error {
	operatorSdkProjectAbsPath, _ := filepath.Abs(sdkInfo.projectName)
	operatorSdkProjectPath := "OPERATOR_SDK_PROJECT_PATH=" + operatorSdkProjectAbsPath
	operatorSdkArgs := "OPERATOR_SDK_ARGS= generate kustomize manifests --interactive=false"
	cmd := exec.Command("make", "operator-sdk", operatorSdkProjectPath, operatorSdkArgs)
	cmd.Dir = operatorControllerHome
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error generating CSV for the operator-sdk project %v: %v: %v", sdkInfo.projectName, string(output), err)
	}

	// Checking if CRD manifests are generated
	csvFilePath := filepath.Join(sdkInfo.projectName, "config", "manifests", "bases", sdkInfo.projectName+".clusterserviceversion.yaml")
	Expect(csvFilePath).To(BeAnExistingFile())

	return nil
}

// generates the bundle directory content for plain bundle format. The yaml files are formed using the kustomize tool
// and the bundle dockerfile is generated using a custom routine.
func kustomizeGenPlainBundleDirectory(sdkInfo *SdkProjectInfo, rootBundlePath string, bundleData BundleContent) error {
	// Create the bundle directory structure
	if err := os.MkdirAll(filepath.Join(rootBundlePath, bundleData.bInputDir, "manifests"), os.ModePerm); err != nil {
		return fmt.Errorf("Failed to create directory for bundle structure %s: %v", bundleData.bInputDir, err)
	}

	// Create the manifests for the plain+v0 bundle
	operatorSdkProjectAbsPath, _ := filepath.Abs(sdkInfo.projectName)
	operatorSdkProjectPath := "OPERATOR_SDK_PROJECT_PATH=" + operatorSdkProjectAbsPath
	outputPlainBundlePath, _ := filepath.Abs(filepath.Join(rootBundlePath, bundleData.bInputDir, "manifests", "manifest.yaml"))
	kustomizeArgs := "KUSTOMIZE_ARGS= build config/default > " + outputPlainBundlePath
	cmd := exec.Command("make", "kustomize", operatorSdkProjectPath, kustomizeArgs)
	cmd.Dir = operatorControllerHome
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error generating plain bundle directory %v: %v", string(output), err)
	}

	// Creates bundle dockerfile
	dockerFilePath := filepath.Join(rootBundlePath, bundleData.bInputDir, "plainbundle.Dockerfile")
	if err = generateBundleDockerFile(dockerFilePath, bundleData.bInputDir); err != nil {
		return fmt.Errorf("Error generating bundle dockerfile for the bundle %v: %v", bundleData.bInputDir, err)
	}
	return nil
}

// Copies the CRDs. Generates metadata and manifest in registry+v1 bundle format.
// Build the bundle image and load into cluster.
// Copies the bundle to appropriate bundle format.
func sdkBundleComplete(sdkInfo *SdkProjectInfo, rootBundlePath string, bundleData BundleContent) error {
	// Copy CRDs and other supported kinds and generate metadata and manifest in bundle format
	bundleGenFlags := "BUNDLE_GEN_FLAGS=-q --overwrite=false --version " + bundleData.bundleVersion + " $(BUNDLE_METADATA_OPTS)"
	cmd := exec.Command("make", "bundle", bundleGenFlags)
	cmd.Dir = sdkInfo.projectName
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error generating bundle format for the bundle %v: %v :%v", bundleData.bInputDir, string(output), err)
	}

	// Check if bundle manifests are created
	bundleManifestPath := filepath.Join(sdkInfo.projectName, "bundle", "manifests")
	Expect(bundleManifestPath).To(BeAnExistingFile())

	// Check if bundle metadata is created
	bundleMetadataPath := filepath.Join(sdkInfo.projectName, "bundle", "metadata")
	Expect(bundleMetadataPath).To(BeAnExistingFile())

	// Build the bundle image
	bundleImg := "BUNDLE_IMG=" + bundleData.imageRef
	cmd = exec.Command("make", "bundle-build", bundleImg)
	cmd.Dir = sdkInfo.projectName
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error building bundle image %v with tag %v :%v: %v", bundleData.imageRef, bundleData.bundleVersion, string(output), err)
	}

	// Push the bundle image
	cmd = exec.Command("make", "bundle-push", bundleImg)
	cmd.Dir = sdkInfo.projectName
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error pushing bundle image %v with tag %v : %v: %v", bundleData.imageRef, bundleData.bundleVersion, string(output), err)
	}

	// Load the bundle image into test environment
	if err = loadImages(GinkgoWriter, kindServer, bundleData.imageRef); err != nil {
		return err
	}

	// Move the bundle structure into correct testdata folder for bundles
	err = moveFolderContents(filepath.Join(sdkInfo.projectName, "bundle"), filepath.Join(rootBundlePath, bundleData.bInputDir))
	Expect(err).ToNot(HaveOccurred())
	Expect(filepath.Join(rootBundlePath, bundleData.bInputDir)).To(BeAnExistingFile())

	// Move the generated dockerfile to correct path
	err = os.Rename(filepath.Join(sdkInfo.projectName, "bundle.Dockerfile"), filepath.Join(rootBundlePath, bundleData.bInputDir, "bundle.Dockerfile"))
	Expect(err).ToNot(HaveOccurred())

	return nil
}

// buildPushLoadContainer function builds a Docker container image using the Docker
// command-line tool, pushes to a container registry and loads into a kind cluster.
//
// The function takes the following arguments and returns back the error if any:
// `tag`: container image tag/name.
// `dockerfilePath`: path to the Dockerfile that defines the container image.
// `dockerContext`: context directory containing the files and resources referenced by the Dockerfile.
// `w`: Writer to which the standard output and standard error will be redirected.
func buildPushLoadContainer(tag, dockerfilePath, dockerContext, kindServer string, w io.Writer) error {
	cmd := exec.Command(containerRuntime, "build", "-t", tag, "-f", dockerfilePath, dockerContext)
	cmd.Stderr = w
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Error building Docker container image %s : %+v", tag, err)
	}

	cmd = exec.Command(containerRuntime, "push", tag)
	cmd.Stderr = w
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Error pushing Docker container image: %s to the registry: %+v", tag, err)
	}

	err := loadImages(w, kindServer, tag)
	return err
}

// loadImages loads container images into a Kubernetes kind cluster
//
// The function takes the `Writer` to which the standard output and standard error will be redirected,
// the kind cluster to which the image is to be loaded, the container image to be loaded.
//
//	and returns back the error if any
func loadImages(w io.Writer, kindServerName string, images ...string) error {
	for _, image := range images {
		cmd := exec.Command("kind", "load", "docker-image", image, "--name", kindServerName)
		cmd.Stderr = w
		cmd.Stdout = w
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Error loading the container image %s into the cluster %s : %+v", image, kindServerName, err)
		}
	}
	return nil
}

// Generates catalog directory contents for the plain bundle format. The FBC template and the Dockerfile
// is formed using custom routines
func genPlainCatalogDirectory(catalogDInfo *CatalogDInfo, imageRefsBundleVersions map[string]string) error {
	// forming the FBC using custom routine
	fbc := CreateFBC(catalogDInfo.operatorName, catalogDInfo.desiredChannelName, imageRefsBundleVersions)
	if err := WriteFBC(*fbc, filepath.Join(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir), catalogDInfo.fbcFileName); err != nil {
		return fmt.Errorf("Error writing FBC content for the fbc %v : %v", catalogDInfo.fbcFileName, err)
	}

	// generating dockerfile for the catalog using custom routine
	dockerFilePath := filepath.Join(catalogDInfo.baseFolderPath, fmt.Sprintf("%s.Dockerfile", catalogDInfo.catalogDir))
	if err := generateCatalogDockerFile(dockerFilePath, catalogDInfo.catalogDir); err != nil {
		return fmt.Errorf("Error generating catalog Dockerfile for the catalog directory %v : %v", catalogDInfo.catalogDir, err)
	}
	return nil
}

// Generates catalog contents for the registry bundle format. The FBC(yaml file) and the Dockerfile
// is formed using opm tool.
func genRegistryCatalogDirectory(catalogDInfo *CatalogDInfo, bundleImageRefs []string, semverTemplateFileName string) error {
	// forming the semver template yaml file
	sdkCatalogFile := filepath.Join(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir, catalogDInfo.fbcFileName)
	if err := formOLMSemverTemplateFile(semverTemplateFileName, bundleImageRefs); err != nil {
		return fmt.Errorf("Error forming the semver template yaml file %v : %v", semverTemplateFileName, err)
	}

	// generating the FBC using semver template")
	semverTemplateFileAbsPath, err := filepath.Abs(semverTemplateFileName)
	if err != nil {
		return fmt.Errorf("Error forming the absolute path of the semver file %v : %v", semverTemplateFileName, err)
	}
	opmArgs := "OPM_ARGS=alpha render-template semver " + semverTemplateFileAbsPath + " -o yaml --use-http"
	cmd := exec.Command("make", "-s", "opm", opmArgs)
	cmd.Dir = operatorControllerHome
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Error running opm command for FBC generation: %v", err)
	}

	// saving the output of semver template under catalogs in testdata
	if err = os.MkdirAll(filepath.Dir(sdkCatalogFile), os.ModePerm); err != nil {
		return fmt.Errorf("Error forming the catalog directory structure: %v", err)
	}
	file, err := os.Create(sdkCatalogFile)
	if err != nil {
		return fmt.Errorf("Error creating the file %v: %v", sdkCatalogFile, err)
	}
	defer file.Close()
	if _, err = file.Write(output); err != nil {
		return fmt.Errorf("Error writing to the file %v: %v", sdkCatalogFile, err)
	}

	// validating the FBC using opm validate
	if err = validateFBC(filepath.Join(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir)); err != nil {
		return fmt.Errorf("Error validating the FBC %v: %v", sdkCatalogFile, err)
	}

	// generating the dockerfile for catalog using opm generate tool
	dockerFolderAbsPath, err := filepath.Abs(filepath.Join(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir))
	if err != nil {
		return fmt.Errorf("Error forming the absolute path of the catalog dockerfile %v", err)
	}
	opmArgs = "OPM_ARGS=generate dockerfile " + dockerFolderAbsPath
	cmd = exec.Command("make", "opm", opmArgs)
	cmd.Dir = operatorControllerHome
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error generating catalog dockerfile : %v :%v", string(output), err)
	}
	return nil
}

// Validates the FBC using opm tool
func validateFBC(fbcDirPath string) error {
	fbcDirAbsPath, err := filepath.Abs(fbcDirPath)
	if err != nil {
		return fmt.Errorf("FBC validation error in absolute path %s: %s", fbcDirPath, err)
	}
	opmArgs := "OPM_ARGS=validate " + fbcDirAbsPath
	cmd := exec.Command("make", "opm", opmArgs)
	cmd.Dir = operatorControllerHome
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("FBC validation failed: %s", output)
	}
	return nil
}

// Creates catalog CR
func createTestCatalog(ctx context.Context, name, imageRef string) (*catalogd.Catalog, error) {
	catalog := &catalogd.Catalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: catalogd.CatalogSpec{
			Source: catalogd.CatalogSource{
				Type: catalogd.SourceTypeImage,
				Image: &catalogd.ImageSource{
					Ref: imageRef,
				},
			},
		},
	}

	err := c.Create(ctx, catalog)
	return catalog, err
}

// Creates the operator for opName for the version
func createOperator(ctx context.Context, opName, version string) (*operatorv1alpha1.Operator, error) {
	operator := &operatorv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: opName,
		},
		Spec: operatorv1alpha1.OperatorSpec{
			PackageName: opName,
			Version:     version,
		},
	}

	err := c.Create(ctx, operator)
	return operator, err

	// err := checkOperatorOperationsSuccess(opName, catalogDInfo.operatorName, operatorAction.installVersion, bundleInfo.baseFolderPath, nameSpace)
}

// Upgrades the operator opName for the version
func upgradeOperator(ctx context.Context, opName, version string) (*operatorv1alpha1.Operator, error) {
	operator := &operatorv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: opName,
		},
	}
	if err := c.Get(ctx, types.NamespacedName{Name: opName}, operator); err != nil {
		return nil, err
	}
	operator.Spec.PackageName = opName
	operator.Spec.Version = version

	err := c.Update(ctx, operator)
	return operator, err
}

// Deletes the operator CR with the name opName
func deleteOperator(ctx context.Context, opName string) error {
	operator := &operatorv1alpha1.Operator{}
	if err := c.Get(ctx, types.NamespacedName{Name: opName}, operator); err != nil {
		return fmt.Errorf("Error deleting the operator %v for the version %v : %v", opName, operator.Spec.Version, err)
	}

	err := c.Delete(ctx, operator)
	return err
}

// Checks if the operator was successfully deleted by trying to reteive the operator with the name opName.
// Error in retrieving indicates a successful deletion.
func validateOperatorDeletion(opName string) error {
	err := c.Get(ctx, types.NamespacedName{Name: opName}, &operatorv1alpha1.Operator{})
	return err
}

// Deletes the catalog CR.
func deleteCatalog(catalog *catalogd.Catalog) error {
	if err := c.Delete(ctx, catalog); err != nil {
		return fmt.Errorf("Error deleting the catalog instance: %v", err)
	}
	return nil
}

// Checks if the catalog was successfully deleted by trying to reteive the catalog.
// Error in retrieving indicates a successful deletion.
func validateCatalogDeletion(catalog *catalogd.Catalog) error {
	err := c.Get(ctx, types.NamespacedName{Name: catalog.Name}, &catalogd.Catalog{})
	return err
}

// Checks if the expected condition and actual condition for a resource matches and returns error if not.
func checkConditionEquals(actualCond, expectedCond *metav1.Condition) error {
	if actualCond == nil {
		return fmt.Errorf("Expected condition %s to not be nil", expectedCond.Type)
	}
	if actualCond.Status != expectedCond.Status {
		return fmt.Errorf("Expected status: %s, but got: %s", expectedCond.Status, actualCond.Status)
	}
	if actualCond.Reason != expectedCond.Reason {
		return fmt.Errorf("Expected reason: %s but got: %s", expectedCond.Reason, actualCond.Reason)
	}
	if !strings.Contains(actualCond.Message, expectedCond.Message) {
		return fmt.Errorf("Expected message: %s but got: %s", expectedCond.Message, actualCond.Message)
	}
	return nil
}

// Checks if the catalog resource is successfully unpacked and returns error if not.
func validateCatalogUnpacking(operatorCatalog *catalogd.Catalog) error {
	if err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog); err != nil {
		return fmt.Errorf("Error retrieving catalog %v:%v", operatorCatalog.Name, err)
	}

	cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
	expectedCond := &metav1.Condition{
		Type:    catalogd.TypeUnpacked,
		Status:  metav1.ConditionTrue,
		Reason:  catalogd.ReasonUnpackSuccessful,
		Message: "successfully unpacked the catalog image",
	}
	if err := checkConditionEquals(cond, expectedCond); err != nil {
		return fmt.Errorf("Status conditions for the catalog instance %v is not as expected:%v", operatorCatalog.Name, err)
	}
	return nil
}

// Creates catalog CR and checks if catalog unpackging is successful and if the packages and bundle metadatas are formed
func createCatalogCheckResources(operatorCatalog *catalogd.Catalog, catalogDInfo *CatalogDInfo, bundleVersions []string) (*catalogd.Catalog, error) {
	operatorCatalog, err := createTestCatalog(ctx, catalogDInfo.catalogDir, catalogDInfo.imageRef)
	if err != nil {
		return nil, fmt.Errorf("Error creating catalog %v : %v", catalogDInfo.catalogDir, err)
	}

	// checking if catalog unpacking is successful
	Eventually(func(g Gomega) {
		err = validateCatalogUnpacking(operatorCatalog)
		g.Expect(err).ToNot(HaveOccurred())
	}, 2*time.Minute, 1).Should(Succeed())

	// checking if the packages are created
	Eventually(func(g Gomega) {
		err = validatePackageCreation(operatorCatalog, catalogDInfo.operatorName)
		g.Expect(err).ToNot(HaveOccurred())
	}, 2*time.Minute, 1).Should(Succeed())

	// checking if the bundle metadatas are created
	By("Eventually checking if bundle metadata is created")
	Eventually(func(g Gomega) {
		err = validateBundleMetadataCreation(operatorCatalog, catalogDInfo.operatorName, bundleVersions)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())
	return operatorCatalog, nil
}

// Checks if the operator operator succeeds following operator install or upgrade
func checkOperatorOperationsSuccess(operator *operatorv1alpha1.Operator, pkgName, opVersion, bundlePath, nameSpace string) {
	// checking for a successful resolution and bundle path
	Eventually(func(g Gomega) {
		err := validateResolutionAndBundlePath(operator)
		g.Expect(err).ToNot(HaveOccurred())
	}, 2*time.Minute, 1).Should(Succeed())

	// checking for a successful operator installation
	Eventually(func(g Gomega) {
		err := validateOperatorInstallation(operator, opVersion)
		g.Expect(err).ToNot(HaveOccurred())
	}, 2*time.Minute, 1).Should(Succeed())

	// checking for a successful package installation
	Eventually(func(g Gomega) {
		err := validatePackageInstallation(operator)
		g.Expect(err).ToNot(HaveOccurred())
	}, 2*time.Minute, 1).Should(Succeed())

	// verifying the presence of relevant manifest from the bundle on cluster
	Eventually(func(g Gomega) {
		err := checkManifestPresence(bundlePath, pkgName, opVersion, nameSpace)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())
}

// Checks if the packages are created from the catalog and returns error if not.
// The expected pkgName is taken as input and is compared against the packages collected whose catalog name
// matches the catalog under consideration.
func validatePackageCreation(operatorCatalog *catalogd.Catalog, pkgName string) error {
	var pkgCollected string
	pList := &catalogd.PackageList{}
	if err := c.List(ctx, pList); err != nil {
		return fmt.Errorf("Error retrieving the packages after %v catalog instance creation: %v", operatorCatalog.Name, err)
	}
	for _, pack := range pList.Items {
		if pack.Spec.Catalog.Name == operatorCatalog.Name {
			pkgCollected = pack.Spec.Name
		}
	}
	if pkgCollected != pkgName {
		return fmt.Errorf("Package %v for the catalog %v is not created", pkgName, operatorCatalog.Name)
	}
	return nil
}

// Checks if the bundle metadatas are created from the catalog and returns error if not.
// The expected pkgNames and their versions are taken as input. This is then compared against the collected bundle versions.
// The collected bundle versions are formed by reading the version from "olm.package" property type whose catalog name
// matches the catalog name and pkgName matches the pkgName under consideration.
func validateBundleMetadataCreation(operatorCatalog *catalogd.Catalog, pkgName string, versions []string) error {
	type Package struct {
		PackageName string `json:"packageName"`
		Version     string `json:"version"`
	}
	var pkgValue Package
	collectedBundleVersions := make([]string, 0)
	bmList := &catalogd.BundleMetadataList{}
	if err := c.List(ctx, bmList); err != nil {
		return fmt.Errorf("Error retrieving the bundle metadata after %v catalog instance creation: %v", operatorCatalog.Name, err)
	}

	for _, bm := range bmList.Items {
		if bm.Spec.Catalog.Name == operatorCatalog.Name {
			for _, prop := range bm.Spec.Properties {
				if prop.Type == "olm.package" {
					err := json.Unmarshal(prop.Value, &pkgValue)
					if err == nil && pkgValue.PackageName == pkgName {
						collectedBundleVersions = append(collectedBundleVersions, pkgValue.Version)
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(collectedBundleVersions, versions) {
		return fmt.Errorf("Package %v for the catalog %v is not created", pkgName, operatorCatalog.Name)
	}

	return nil
}

// Checks for a successful resolution and bundle path for the operator and returns error if not.
func validateResolutionAndBundlePath(operator *operatorv1alpha1.Operator) error {
	if err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator); err != nil {
		return fmt.Errorf("Error retrieving operator %v:%v", operator.Name, err)
	}
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
	expectedCond := &metav1.Condition{
		Type:    operatorv1alpha1.TypeResolved,
		Status:  metav1.ConditionTrue,
		Reason:  operatorv1alpha1.ReasonSuccess,
		Message: "resolved to",
	}
	if err := checkConditionEquals(cond, expectedCond); err != nil {
		return fmt.Errorf("Status conditions for the operator %v for the version %v is not as expected:%v", operator.Name, operator.Spec.Version, err)
	}
	if operator.Status.ResolvedBundleResource == "" {
		return fmt.Errorf("Resoved Bundle Resource is not found for the operator %v for the version %v", operator.Name, operator.Spec.Version)
	}
	return nil
}

// Checks if the operator installation succeeded and returns error if not.
func validateOperatorInstallation(operator *operatorv1alpha1.Operator, operatorVersion string) error {
	if err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator); err != nil {
		return fmt.Errorf("Error retrieving operator %v:%v", operator.Name, err)
	}
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
	expectedCond := &metav1.Condition{
		Type:    operatorv1alpha1.TypeInstalled,
		Status:  metav1.ConditionTrue,
		Reason:  operatorv1alpha1.ReasonSuccess,
		Message: "installed from",
	}
	if err := checkConditionEquals(cond, expectedCond); err != nil {
		return fmt.Errorf("Status conditions for the operator %v for the version %v is not as expected:%v", operator.Name, operator.Spec.Version, err)
	}
	if operator.Status.InstalledBundleResource == "" {
		return fmt.Errorf("Installed Bundle Resource is not found for the operator %v for the version %v", operator.Name, operator.Spec.Version)
	}
	if operator.Spec.Version != operatorVersion {
		return fmt.Errorf("Expected operator version: %s for the operator %v, but got: %s", operator.Spec.Version, operator.Name, operatorVersion)
	}
	return nil
}

// Checks if bundle deployment succeeded and returns error if not.
func validatePackageInstallation(operator *operatorv1alpha1.Operator) error {
	bd := rukpakv1alpha1.BundleDeployment{}
	if err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, &bd); err != nil {
		return fmt.Errorf("Error retrieving the bundle deployments for the operator %v:%v", operator.Name, err)
	}
	cond := apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
	expectedCond := &metav1.Condition{
		Type:    rukpakv1alpha1.TypeHasValidBundle,
		Status:  metav1.ConditionTrue,
		Reason:  rukpakv1alpha1.ReasonUnpackSuccessful,
		Message: "Successfully unpacked",
	}
	if err := checkConditionEquals(cond, expectedCond); err != nil {
		return fmt.Errorf("Status conditions of the bundle deployment for the operator %v for the version %v is not as expected:%v", operator.Name, operator.Spec.Version, err)
	}

	cond = apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeInstalled)
	expectedCond = &metav1.Condition{
		Type:    rukpakv1alpha1.TypeInstalled,
		Status:  metav1.ConditionTrue,
		Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
		Message: "Instantiated bundle",
	}
	if err := checkConditionEquals(cond, expectedCond); err != nil {
		return fmt.Errorf("Status conditions of the bundle deployment for the operator %v for the version %v is not as expected:%v", operator.Name, operator.Spec.Version, err)
	}

	return nil
}

// Checks the presence of operator manifests for the operator
func checkManifestPresence(bundlePath, operatorName, version, namespace string) error {
	resources, err := collectKubernetesObjects(bundlePath, operatorName, version)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if resource.GetObjectKind().GroupVersionKind().Kind == "ClusterServiceVersion" {
			continue
		}
		gvk := resource.GetObjectKind().GroupVersionKind()
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		objMeta, ok := resource.(metav1.Object)
		if !ok {
			return fmt.Errorf("Failed to convert resource to metav1.Object")
		}
		objName := objMeta.GetName()
		namespacedName := types.NamespacedName{
			Name:      objName,
			Namespace: namespace,
		}
		if err = c.Get(ctx, namespacedName, obj); err != nil {
			return fmt.Errorf("Error retrieving the resources %v from the namespace %v: %v", namespacedName.Name, namespace, err)
		}
	}
	return nil
}

// Moves the content from currentPath to newPath
func moveFolderContents(currentPath, newPath string) error {
	files, err := os.ReadDir(currentPath)
	if err != nil {
		return fmt.Errorf("Failed to read the folder %s: %v", currentPath, err)
	}

	for _, file := range files {
		oldPath := filepath.Join(currentPath, file.Name())
		newFilePath := filepath.Join(newPath, file.Name())

		if err = os.MkdirAll(filepath.Dir(newFilePath), os.ModePerm); err != nil {
			return fmt.Errorf("Failed to create directory for file %s: %v", file.Name(), err)
		}

		if err = os.Rename(oldPath, newFilePath); err != nil {
			return fmt.Errorf("Failed to move file %s: %v", file.Name(), err)
		}
	}

	return nil
}

// Delete the folders or file in the collection array
func deleteFolderFile(collection []string) error {
	for _, c := range collection {
		if err := os.RemoveAll(c); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("Error deleting %v:%v", c, err)
			}
		}
	}
	return nil
}
