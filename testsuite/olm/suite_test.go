package olm

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	utils "github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils"
	suite "github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils/suite"
	"github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils/types"
)

var log = logf.Log.WithName("olm-testsuite")

var suiteCtx *types.SuiteContext

func init() {
	suite.SetFlags()

	if utils.OLMCatalogSourceNamespace == "" {
		utils.OLMCatalogSourceNamespace = utils.OperatorNamespace
	}

}

func TestApicurioE2E(t *testing.T) {
	suiteCtx = suite.NewSuiteContext("olm")
	suite.RunSuite(t, "Operator OLM Testsuite", suiteCtx)
}

var olminfo *OLMInstallationInfo

var _ = BeforeSuite(func(done Done) {

	suite.InitSuite(suiteCtx)
	Expect(suiteCtx).ToNot(BeNil())

	olminfo = installOperatorOLM()

	close(done)

}, 15*60)

var _ = AfterSuite(func() {

	uninstallOperatorOLM(olminfo)

	suite.TearDownSuite(suiteCtx)

}, 5*60)
