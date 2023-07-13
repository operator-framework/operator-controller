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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var (
	cfg *rest.Config
	c   client.Client
	ctx context.Context
)

type BundleInfo struct {
	baseFolderPath string          // Base path of the folder for the specific the bundle type input data
	bundles        []BundleContent // Stores the data relevant to different versions of the bundle
}

type BundleContent struct {
	bInputDir     string // The input directory containing the specific version of bundle data
	bundleVersion string // The specific version of the bundle data
	imageRef      string // Stores the bundle image reference
}

type CatalogDInfo struct {
	baseFolderPath     string // Base path of the folder for the catalogs
	catalogDir         string // The folder storing the FBC
	operatorName       string // Name of the operator to be installed from the bundles
	desiredChannelName string // Desired channel name for the operator
	imageRef           string // Stores the FBC image reference
	fbcFileName        string // Name of the FBC file
}

type OperatorActionInfo struct {
	installVersion string // Version of the operator to be installed on the cluster
	upgradeVersion string // Version of the operator to be upgraded on the cluster
}

type SdkProjectInfo struct {
	projectName string
	domainName  string
	group       string
	version     string
	kind        string
}

const (
	remoteRegistryRepo = "localhost:5000/"
	kindServer         = "operator-controller-e2e"
	opmPath            = "../../bin/opm"
	deployedNameSpace  = "rukpak-system"
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
	Expect(err).To(Not(HaveOccurred()))

	err = rukpakv1alpha1.AddToScheme(scheme)
	Expect(err).To(Not(HaveOccurred()))

	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).To(Not(HaveOccurred()))

	ctx = context.Background()

})

var _ = Describe("Operator Framework E2E for plain bundles", func() {
	var (
		bundleInfo      *BundleInfo
		catalogDInfo    *CatalogDInfo
		operatorAction  *OperatorActionInfo
		operatorCatalog *catalogd.Catalog
		operator        *operatorv1alpha1.Operator
		err             error
	)
	BeforeEach(func() {
		bundleInfo = &BundleInfo{
			baseFolderPath: "../../testdata/bundles/plain-v0",
			bundles: []BundleContent{
				{
					bInputDir:     "plain.v0.1.0",
					bundleVersion: "0.1.0",
				},
				{
					bInputDir:     "plain.v0.1.1",
					bundleVersion: "0.1.1",
				},
			},
		}
		catalogDInfo = &CatalogDInfo{
			baseFolderPath:     "../../testdata/catalogs",
			fbcFileName:        "catalog.yaml",
			operatorName:       "plain",
			desiredChannelName: "beta",
		}
		operatorAction = &OperatorActionInfo{
			installVersion: "0.1.0",
			upgradeVersion: "0.1.1",
		}
		for i, b := range bundleInfo.bundles {
			bundleInfo.bundles[i].imageRef = remoteRegistryRepo + catalogDInfo.operatorName + "-bundle:v" + b.bundleVersion
		}
		catalogDInfo.catalogDir = catalogDInfo.operatorName + "-catalog"
		catalogDInfo.imageRef = remoteRegistryRepo + catalogDInfo.catalogDir + ":test"
	})
	When("Build and load plain+v0 bundle images into the test environment", func() {
		It("Build the plain bundle images and load them", func() {
			for _, b := range bundleInfo.bundles {
				dockerContext := bundleInfo.baseFolderPath + "/" + b.bInputDir
				dockerfilePath := dockerContext + "/Dockerfile"
				err = buildPushLoadContainer(b.imageRef, dockerfilePath, dockerContext, kindServer, GinkgoWriter)
				Expect(err).To(Not(HaveOccurred()))
			}
		})
	})
	When("Create the FBC", func() {
		It("Create a FBC", func() {
			By("Creating FBC for plain bundle using custom routine")
			var imageRefs []string
			var bundleVersions []string
			for _, b := range bundleInfo.bundles {
				imageRefs = append(imageRefs, b.imageRef)
				bundleVersions = append(bundleVersions, b.bundleVersion)
			}
			fbc := CreateFBC(catalogDInfo.operatorName, catalogDInfo.desiredChannelName, imageRefs, bundleVersions)
			err = WriteFBC(*fbc, catalogDInfo.baseFolderPath+"/"+catalogDInfo.catalogDir, catalogDInfo.fbcFileName)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("Build and load the FBC image into the test environment", func() {
		It("Generate the docker file, build and load FBC image", func() {
			By("Calling generate dockerfile function written")
			err = generateDockerFile(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir, catalogDInfo.catalogDir+".Dockerfile")
			Expect(err).To(Not(HaveOccurred()))

			By("Building the catalog image and loading into the test environment")
			dockerContext := catalogDInfo.baseFolderPath
			dockerfilePath := dockerContext + "/" + catalogDInfo.catalogDir + ".Dockerfile"
			err = buildPushLoadContainer(catalogDInfo.imageRef, dockerfilePath, dockerContext, kindServer, GinkgoWriter)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("Create a catalog object and check if the resources are created", func() {
		It("Create catalog object for the FBC and check if catalog, packages and bundle metadatas are created", func() {
			bundleVersions := make([]string, len(bundleInfo.bundles))
			for i, bundle := range bundleInfo.bundles {
				bundleVersions[i] = bundle.bundleVersion
			}
			operatorCatalog, err = createCatalogCheckResources(operatorCatalog, catalogDInfo, bundleVersions)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("Install an operator and check if the operator operations succeed", func() {
		It("Create an operator object and install it", func() {
			By("Creating an operator object")
			operator, err = createOperator(ctx, catalogDInfo.operatorName, operatorAction.installVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Checking if the operator operations suceceded")
			checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.installVersion, bundleInfo.baseFolderPath)
		})
	})
	When("Upgrade an operator to higher version and check if the operator operations succeed", func() {
		It("Upgrade to a higher version of the operator", func() {
			By("Upgrading the operator")
			operator, err = upgradeOperator(ctx, catalogDInfo.operatorName, operatorAction.upgradeVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Checking if the operator operations succeeded")
			checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.upgradeVersion, bundleInfo.baseFolderPath)
		})
	})
	When("Delete an operator", func() {
		It("Delete an operator", func() {
			err = deleteOperator(ctx, catalogDInfo.operatorName)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the operator doesn't exist")
			Eventually(func(g Gomega) {
				err = checkOperatorDeleted(catalogDInfo.operatorName)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
	When("Clearing up catalog object and other files formed for the test", func() {
		It("Clearing up data generated for the test", func() {
			//Deleting the catalog object and checking if the deletion was successful
			Eventually(func(g Gomega) {
				err = deleteAndCheckCatalogDeleted(operatorCatalog)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())

			var toDelete []string
			toDelete = append(toDelete, catalogDInfo.baseFolderPath+"/"+catalogDInfo.catalogDir)               // delete the FBC formed
			toDelete = append(toDelete, catalogDInfo.baseFolderPath+"/"+catalogDInfo.catalogDir+".Dockerfile") // delete the catalog Dockerfile generated
			err = deleteFolderFile(toDelete)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("Operator Framework E2E for registry+v1 bundles", func() {
	var (
		sdkInfo         *SdkProjectInfo
		bundleInfo      *BundleInfo
		catalogDInfo    *CatalogDInfo
		operatorAction  *OperatorActionInfo
		operatorCatalog *catalogd.Catalog
		operator        *operatorv1alpha1.Operator
		semverFileName  string
		err             error
	)
	BeforeEach(func() {
		sdkInfo = &SdkProjectInfo{
			projectName: "example-operator",
			domainName:  "example.com",
			group:       "cache",
			version:     "v1alpha1",
			kind:        "Memcached1",
		}
		bundleInfo = &BundleInfo{
			baseFolderPath: "../../testdata/bundles/registry-v1",
			bundles: []BundleContent{
				{
					bundleVersion: "0.1.0",
				},
				{
					bundleVersion: "0.1.1",
				},
			},
		}
		catalogDInfo = &CatalogDInfo{
			baseFolderPath: "../../testdata/catalogs",
			fbcFileName:    "catalog.yaml",
			operatorName:   "example-operator",
		}
		operatorAction = &OperatorActionInfo{
			installVersion: "0.1.0",
			upgradeVersion: "0.1.1",
		}
		for i, b := range bundleInfo.bundles {
			bundleInfo.bundles[i].bInputDir = sdkInfo.projectName + ".v" + b.bundleVersion
			bundleInfo.bundles[i].imageRef = remoteRegistryRepo + sdkInfo.projectName + "-bundle:v" + b.bundleVersion
		}
		catalogDInfo.catalogDir = catalogDInfo.operatorName + "-catalog"
		catalogDInfo.imageRef = remoteRegistryRepo + catalogDInfo.catalogDir + ":test"

		semverFileName = "registry-semver.yaml"
	})

	When("Build registry+v1 bundles with operator-sdk", func() {
		It("Initialize new operator-sdk project and create new api and controller", func() {
			err = sdkInitialize(sdkInfo)
			Expect(err).NotTo(HaveOccurred())
		})
		It("Generate manifests and CSV for the operator", func() {
			err = sdkGenerateManifestsCSV(sdkInfo)
			Expect(err).NotTo(HaveOccurred())
		})
		It("Generate and build registry+v1 bundle", func() {
			// Creates bundle structure for the specified bundle versions
			// Bundle content is same for the bundles now
			for _, b := range bundleInfo.bundles {
				err = sdkComplete(sdkInfo, bundleInfo.baseFolderPath, b)
				Expect(err).NotTo(HaveOccurred())
			}
		})
	})
	When("Create FBC and validate FBC", func() {
		It("Create a FBC", func() {
			sdkCatalogFile := filepath.Join(catalogDInfo.baseFolderPath, catalogDInfo.catalogDir, catalogDInfo.fbcFileName)
			By("Forming the semver yaml file")
			bundleImageRefs := make([]string, len(bundleInfo.bundles))
			for i, bundle := range bundleInfo.bundles {
				bundleImageRefs[i] = bundle.imageRef
			}
			err := generateOLMSemverFile(semverFileName, bundleImageRefs)
			Expect(err).ToNot(HaveOccurred())

			By("Forming the FBC using semver")
			cmd := exec.Command(opmPath, "alpha", "render-template", "semver", semverFileName, "-o", "yaml", "--use-http")
			output, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred())

			By("Saving the output under catalogs in testdata")
			err = os.MkdirAll(filepath.Dir(sdkCatalogFile), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			file, err := os.Create(sdkCatalogFile)
			Expect(err).ToNot(HaveOccurred())
			defer file.Close()
			_, err = file.Write(output)
			Expect(err).ToNot(HaveOccurred())
		})
		It("Validate FBC", func() {
			By("By validating the FBC using opm validate")
			err = validateFBC(catalogDInfo.baseFolderPath + "/" + catalogDInfo.catalogDir)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("Generate docker file and FBC image and load the FBC image into test environment", func() {
		It("Create the docker file", func() {
			By("By using opm generate tool")
			dockerFolderPath := catalogDInfo.baseFolderPath + "/" + catalogDInfo.catalogDir
			cmd := exec.Command(opmPath, "generate", "dockerfile", dockerFolderPath)
			err = cmd.Run()
			Expect(err).ToNot(HaveOccurred())

			By("Building the catalog image and loading into the test environment")
			dockerContext := catalogDInfo.baseFolderPath
			dockerFilePath := dockerContext + "/" + catalogDInfo.catalogDir + ".Dockerfile"
			err = buildPushLoadContainer(catalogDInfo.imageRef, dockerFilePath, dockerContext, kindServer, GinkgoWriter)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("Create a catalog object and check if the resources are created", func() {
		It("Create catalog object for the FBC and check if catalog, packages and bundle metadatas are created", func() {
			bundleVersions := make([]string, len(bundleInfo.bundles))
			for i, bundle := range bundleInfo.bundles {
				bundleVersions[i] = bundle.bundleVersion
			}
			operatorCatalog, err = createCatalogCheckResources(operatorCatalog, catalogDInfo, bundleVersions)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
	When("Install an operator and check if the operator operations succeed", func() {
		It("Create an operator object and install it", func() {
			By("Creating an operator object")
			operator, err = createOperator(ctx, catalogDInfo.operatorName, operatorAction.installVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Checking if the operator operations succeeded")
			checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.installVersion, bundleInfo.baseFolderPath)
		})
	})
	When("Upgrade an operator to higher version and check if the operator operations succeed", func() {
		It("Upgrade to a higher version of the operator", func() {
			By("Upgrading the operator")
			operator, err = upgradeOperator(ctx, catalogDInfo.operatorName, operatorAction.upgradeVersion)
			Expect(err).ToNot(HaveOccurred())

			By("Checking if the operator operations succeeded")
			checkOperatorOperationsSuccess(operator, catalogDInfo.operatorName, operatorAction.upgradeVersion, bundleInfo.baseFolderPath)
		})
	})
	When("An operator is deleted", func() {
		It("Delete and operator", func() {
			err = deleteOperator(ctx, catalogDInfo.operatorName)
			Expect(err).ToNot(HaveOccurred())

			By("Eventually the operator should not exists")
			Eventually(func(g Gomega) {
				err = checkOperatorDeleted(catalogDInfo.operatorName)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
	When("Clearing up catalog object and other files formed for the test", func() {
		It("Clearing up data generated for the test", func() {
			//Deleting the catalog object and checking if the deletion was successful
			Eventually(func(g Gomega) {
				err = deleteAndCheckCatalogDeleted(operatorCatalog)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())

			var toDelete []string
			for _, b := range bundleInfo.bundles {
				toDelete = append(toDelete, bundleInfo.baseFolderPath+"/"+b.bInputDir) // delete the registry+v1 bundles formed
			}
			toDelete = append(toDelete, sdkInfo.projectName)                                                   //delete the sdk project directory
			toDelete = append(toDelete, semverFileName)                                                        // delete the semver yaml formed
			toDelete = append(toDelete, catalogDInfo.baseFolderPath+"/"+catalogDInfo.catalogDir)               // delete the FBC formed
			toDelete = append(toDelete, catalogDInfo.baseFolderPath+"/"+catalogDInfo.catalogDir+".Dockerfile") // delete the catalog Dockerfile generated
			err = deleteFolderFile(toDelete)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func sdkInitialize(sdkInfo *SdkProjectInfo) error {
	// Create new project for the operator
	err := os.Mkdir(sdkInfo.projectName, 0755)
	if err != nil {
		return fmt.Errorf("Error creating the sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Initialize the operator-sdk project
	domain := "--domain=" + sdkInfo.domainName
	cmd := exec.Command("operator-sdk", "init", domain)
	cmd.Dir = sdkInfo.projectName
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Error initializing the operator-sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Create new API and controller
	group := "--group=" + sdkInfo.group
	version := "--version=" + sdkInfo.version
	kind := "--kind=" + sdkInfo.kind
	cmd = exec.Command("operator-sdk", "create", "api", group, version, kind, "--resource", "--controller")
	cmd.Dir = sdkInfo.projectName
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Error creating new API and controller for the operator-sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Checking if the API was created in the expected path
	apiFilePath := filepath.Join(sdkInfo.projectName, "api", sdkInfo.version, strings.ToLower(sdkInfo.kind)+"_types.go")
	Expect(apiFilePath).To(BeAnExistingFile())

	// Checking if the controller was created in the expected path")
	controllerFilePath := filepath.Join(sdkInfo.projectName, "controllers", strings.ToLower(sdkInfo.kind)+"_controller.go")
	Expect(controllerFilePath).To(BeAnExistingFile())

	return nil
}

func sdkGenerateManifestsCSV(sdkInfo *SdkProjectInfo) error {
	// Update the generated code for the resources
	cmd := exec.Command("make", "generate")
	cmd.Dir = sdkInfo.projectName
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Error updating generated code for the operator-sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Generate and update the CRD manifests
	cmd = exec.Command("make", "manifests")
	cmd.Dir = sdkInfo.projectName
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Error generating and updating crd manifests for the operator-sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Checking if CRD manifests are generated
	crdFilePath := filepath.Join(sdkInfo.projectName, "config", "crd", "bases", sdkInfo.group+"."+sdkInfo.domainName+"_"+strings.ToLower(sdkInfo.kind)+"s.yaml")
	Expect(crdFilePath).To(BeAnExistingFile())

	// Generate CSV for the bundle with default values
	cmd = exec.Command("operator-sdk", "generate", "kustomize", "manifests", "--interactive=false")
	cmd.Dir = sdkInfo.projectName
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Error generating CSV for the operator-sdk project %v:%v", sdkInfo.projectName, err)
	}

	// Checking if CRD manifests are generated
	csvFilePath := filepath.Join(sdkInfo.projectName, "config", "manifests", "bases", sdkInfo.projectName+".clusterserviceversion.yaml")
	Expect(csvFilePath).To(BeAnExistingFile())

	return nil
}

func sdkComplete(sdkInfo *SdkProjectInfo, rootBundlePath string, bundleData BundleContent) error {
	// Copy CRDs and other supported kinds, generate metadata, and in bundle format
	bundleGenFlags := "BUNDLE_GEN_FLAGS=-q --overwrite=false --version " + bundleData.bundleVersion + " $(BUNDLE_METADATA_OPTS)"
	cmd := exec.Command("make", "bundle", bundleGenFlags)
	cmd.Dir = sdkInfo.projectName
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Error generating bundle format for the bundle %v:%v", bundleData.bInputDir, err)
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
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Error building bundle image %v with tag %v :%v", bundleData.imageRef, bundleData.bundleVersion, err)
	}

	// Push the bundle image
	cmd = exec.Command("make", "bundle-push", bundleImg)
	cmd.Dir = sdkInfo.projectName
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Error pushing bundle image %v with tag %v :%v", bundleData.imageRef, bundleData.bundleVersion, err)
	}

	// Load the bundle image into test environment
	err = loadImages(GinkgoWriter, kindServer, bundleData.imageRef)
	if err != nil {
		return err
	}

	// Move the bundle structure into correct testdata folder for bundles
	err = moveFolderContents(sdkInfo.projectName+"/"+"bundle", rootBundlePath+"/"+bundleData.bInputDir)
	Expect(err).NotTo(HaveOccurred())
	Expect(rootBundlePath + "/" + bundleData.bInputDir).To(BeAnExistingFile())

	// Move the generated dockerfile to correct path
	err = os.Rename(sdkInfo.projectName+"/"+"bundle.Dockerfile", rootBundlePath+"/"+bundleData.bInputDir+"/"+"bundle.Dockerfile")
	Expect(err).NotTo(HaveOccurred())

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
	cmd := exec.Command("docker", "build", "-t", tag, "-f", dockerfilePath, dockerContext)
	cmd.Stderr = w
	cmd.Stdout = w
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Error building Docker container image %s : %v", tag, err)
	}

	cmd = exec.Command("docker", "push", tag)
	cmd.Stderr = w
	cmd.Stdout = w
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Error pushing Docker container image: %s to the registry: %v", tag, err)
	}

	err = loadImages(w, kindServer, tag)
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
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("Error loading the container image %s into the cluster %s : %v", image, kindServerName, err)
		}
	}
	return nil
}

// Validates the FBC using opm tool
func validateFBC(fbcDirPath string) error {
	cmd := exec.Command(opmPath, "validate", fbcDirPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("FBC validation failed: %s", output)
	}
	return nil
}

// Creates catalog instance
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

// Creates the operator opName for the version
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
}

// Upgrades the operator opName for the version
func upgradeOperator(ctx context.Context, opName, version string) (*operatorv1alpha1.Operator, error) {
	operator := &operatorv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: opName,
		},
	}
	err := c.Get(ctx, types.NamespacedName{Name: opName}, operator)
	if err != nil {
		return nil, err
	}
	operator.Spec.PackageName = opName
	operator.Spec.Version = version

	err = c.Update(ctx, operator)
	return operator, err
}

// Deletes the operator opName
func deleteOperator(ctx context.Context, opName string) error {
	operator := &operatorv1alpha1.Operator{}
	err := c.Get(ctx, types.NamespacedName{Name: opName}, operator)
	if err != nil {
		return fmt.Errorf("Error deleting the operator %v for the version %v : %v", opName, operator.Spec.Version, err)
	}

	err = c.Delete(ctx, operator)
	return err
}

// Checks if the expected condition and actual condition for a resource matches
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

// Checks if the catalog resource is successfully unpacked
func checkCatalogUnpacked(operatorCatalog *catalogd.Catalog) error {
	err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)
	if err != nil {
		return fmt.Errorf("Error retrieving catalog %v:%v", operatorCatalog.Name, err)
	}

	cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
	expectedCond := &metav1.Condition{
		Type:    catalogd.TypeUnpacked,
		Status:  metav1.ConditionTrue,
		Reason:  catalogd.ReasonUnpackSuccessful,
		Message: "successfully unpacked the catalog image",
	}
	err = checkConditionEquals(cond, expectedCond)
	if err != nil {
		return fmt.Errorf("Status conditions for the catalog instance %v is not as expected:%v", operatorCatalog.Name, err)
	}
	return nil
}

// Creates catalog instance and check if catalog unpackging is successul and if  the packages and bundle metadatas are formed
func createCatalogCheckResources(operatorCatalog *catalogd.Catalog, catalogDInfo *CatalogDInfo, bundleVersions []string) (*catalogd.Catalog, error) {
	operatorCatalog, err := createTestCatalog(ctx, catalogDInfo.catalogDir, catalogDInfo.imageRef)
	if err != nil {
		return nil, fmt.Errorf("Error creating catalog %v : %v", catalogDInfo.catalogDir, err)
	}

	// checking if catalog unpacking is successful
	Eventually(func(g Gomega) {
		err = checkCatalogUnpacked(operatorCatalog)
		g.Expect(err).ToNot(HaveOccurred())
	}, 20*time.Second, 1).Should(Succeed())

	// checking if the packages are created
	Eventually(func(g Gomega) {
		err = checkPackageCreated(operatorCatalog, catalogDInfo.operatorName)
		g.Expect(err).ToNot(HaveOccurred())
	}, 20*time.Second, 1).Should(Succeed())

	// checking if the bundle metadatas are created
	By("Eventually checking if bundle metadata is created")
	Eventually(func(g Gomega) {
		err = checkBundleMetadataCreated(operatorCatalog, catalogDInfo.operatorName, bundleVersions)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())
	return operatorCatalog, nil
}

// Checks if the operator operator succeeds following operator install or upgrade
func checkOperatorOperationsSuccess(operator *operatorv1alpha1.Operator, pkgName, opVersion, bundlePath string) {
	// checking for a successful resolution and bundle path
	Eventually(func(g Gomega) {
		err := checkResolutionAndBundlePath(operator)
		g.Expect(err).ToNot(HaveOccurred())
	}, 15*time.Second, 1).Should(Succeed())

	// checking for a successful operator installation
	Eventually(func(g Gomega) {
		err := checkOperatorInstalled(operator, opVersion)
		g.Expect(err).ToNot(HaveOccurred())
	}, 15*time.Second, 1).Should(Succeed())

	// checking for a successful package installation
	Eventually(func(g Gomega) {
		err := checkPackageInstalled(operator)
		g.Expect(err).ToNot(HaveOccurred())
	}, 15*time.Second, 1).Should(Succeed())

	// verifying the presence of relevant manifest from the bundle on cluster
	Eventually(func(g Gomega) {
		err := checkManifestPresence(bundlePath, pkgName, opVersion)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())
}

// Checks if the packages are created from the catalog.
// The expected pkgName is taken as input and is compared against the packages collected whose catalog name
// matches the catalog under consideration.
func checkPackageCreated(operatorCatalog *catalogd.Catalog, pkgName string) error {
	var pkgCollected string
	pList := &catalogd.PackageList{}
	err := c.List(ctx, pList)
	if err != nil {
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

// Checks if the bundle metadatas are created from the catalog.
// The expected pkgNames and their versions are taken as input. This is then compared against the collected bundle versions.
// The collected bundle versions are formed by reading the version from "olm.package" property type whose catalog name
// matches the catalog name and pkgName matches the pkgName under consideration.
func checkBundleMetadataCreated(operatorCatalog *catalogd.Catalog, pkgName string, versions []string) error {
	type Package struct {
		PackageName string `json:"packageName"`
		Version     string `json:"version"`
	}
	var pkgValue Package
	collectedBundleVersions := make([]string, 0)
	bmList := &catalogd.BundleMetadataList{}
	err := c.List(ctx, bmList)
	if err != nil {
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

// Checks for a successful resolution and bundle path for the operator
func checkResolutionAndBundlePath(operator *operatorv1alpha1.Operator) error {
	err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
	if err != nil {
		return fmt.Errorf("Error retrieving operator %v:%v", operator.Name, err)
	}
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
	expectedCond := &metav1.Condition{
		Type:    operatorv1alpha1.TypeResolved,
		Status:  metav1.ConditionTrue,
		Reason:  operatorv1alpha1.ReasonSuccess,
		Message: "resolved to",
	}
	err = checkConditionEquals(cond, expectedCond)
	if err != nil {
		return fmt.Errorf("Status conditions for the operator %v for the version %v is not as expected:%v", operator.Name, operator.Spec.Version, err)
	}
	if operator.Status.ResolvedBundleResource == "" {
		return fmt.Errorf("Resoved Bundle Resource is not found for the operator %v for the version %v", operator.Name, operator.Spec.Version)
	}
	return nil
}

// Checks if the operator installation succeeded
func checkOperatorInstalled(operator *operatorv1alpha1.Operator, operatorVersion string) error {
	err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
	if err != nil {
		return fmt.Errorf("Error retrieving operator %v:%v", operator.Name, err)
	}
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
	expectedCond := &metav1.Condition{
		Type:    operatorv1alpha1.TypeResolved,
		Status:  metav1.ConditionTrue,
		Reason:  operatorv1alpha1.ReasonSuccess,
		Message: "installed from",
	}
	err = checkConditionEquals(cond, expectedCond)
	if err != nil {
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

// Checks if bundle deployment succeeded
func checkPackageInstalled(operator *operatorv1alpha1.Operator) error {
	bd := rukpakv1alpha1.BundleDeployment{}
	err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, &bd)
	if err != nil {
		return fmt.Errorf("Error retrieving the bundle deployments for the operator %v:%v", operator.Name, err)
	}
	if len(bd.Status.Conditions) != 2 {
		return fmt.Errorf("Two conditions for successful unpack and successful installation for the operater %v for version %v are not populated", operator.Name, operator.Spec.Version)
	}
	if bd.Status.Conditions[0].Reason != "UnpackSuccessful" {
		return fmt.Errorf("Expected status condition reason for successful unpack is not populated for the operater %v for version %v are not populated", operator.Name, operator.Spec.Version)
	}
	if bd.Status.Conditions[1].Reason != "InstallationSucceeded" {
		return fmt.Errorf("Expected status condition reason for successful installation is not populated for the operater %v for version %v are not populated", operator.Name, operator.Spec.Version)
	}
	return nil
}

// Checks the presence of operator manifests for the operator
func checkManifestPresence(bundlePath, operatorName, version string) error {
	resources, err := collectKubernetesObjects(bundlePath, operatorName, version)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if resource.Kind == "ClusterServiceVersion" {
			continue
		}
		gvk := schema.GroupVersionKind{
			Group:   "",
			Version: resource.APIVersion,
			Kind:    resource.Kind,
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		err = c.Get(ctx, types.NamespacedName{Name: resource.Metadata.Name, Namespace: deployedNameSpace}, obj)
		if err != nil {
			return fmt.Errorf("Error retrieving the resources %v from the namespace %v: %v", resource.Metadata.Name, deployedNameSpace, err)
		}
	}
	return nil
}

// Checks if the operator was successfully deleted
func checkOperatorDeleted(opName string) error {
	err := c.Get(ctx, types.NamespacedName{Name: opName}, &operatorv1alpha1.Operator{})
	return err
}

// Deletes the catalog and checks if the deletion was successful
func deleteAndCheckCatalogDeleted(catalog *catalogd.Catalog) error {
	err := c.Delete(ctx, catalog)
	if err != nil {
		return fmt.Errorf("Error deleting the catalog instance: %v", err)
	}
	err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, &catalogd.Catalog{})
	return err
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

		err = os.MkdirAll(filepath.Dir(newFilePath), os.ModePerm)
		if err != nil {
			return fmt.Errorf("Failed to create directory for file %s: %v", file.Name(), err)
		}

		err = os.Rename(oldPath, newFilePath)
		if err != nil {
			return fmt.Errorf("Failed to move file %s: %v", file.Name(), err)
		}
	}

	return nil
}

// Delete the folders or file in the collection array
func deleteFolderFile(collection []string) error {
	for _, c := range collection {
		err := os.RemoveAll(c)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("Error deleting %v:%v", c, err)
			}
		}
	}
	return nil
}
