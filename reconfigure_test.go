// +build !integration

package main

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

type ReconfigureTestSuite struct {
	suite.Suite
	ServiceReconfigure
	ConsulAddress     string
	ConsulTemplateFe  string
	ConsulTemplateBe  string
	ConfigsPath       string
	TemplatesPath     string
	reconfigure       Reconfigure
	Pid               string
	Server            *httptest.Server
	PutPathResponse   string
	ConsulRequestBody ServiceReconfigure
}

func (s *ReconfigureTestSuite) SetupTest() {
	s.Pid = "123"
	s.ServicePath = []string{"path/to/my/service/api", "path/to/my/other/service/api"}
	s.ServiceDomain = "my-domain.com"
	s.ConfigsPath = "path/to/configs/dir"
	s.TemplatesPath = "test_configs/tmpl"
	s.ConsulTemplateFe = `
    acl url_myService path_beg path/to/my/service/api path_beg path/to/my/other/service/api
    use_backend myService-be if url_myService`
	s.ConsulTemplateBe = `backend myService-be
    {{range $i, $e := service "myService" "any"}}
    server {{$e.Node}}_{{$i}}_{{$e.Port}} {{$e.Address}}:{{$e.Port}} check
    {{end}}`
	cmdRunHa = func(cmd *exec.Cmd) error {
		return nil
	}
	cmdRunConsul = func(cmd *exec.Cmd) error {
		return nil
	}
	readPidFile = func(fileName string) ([]byte, error) {
		return []byte(s.Pid), nil
	}
	writeConsulTemplateFile = func(fileName string, data []byte, perm os.FileMode) error {
		return nil
	}
	s.ConsulAddress = s.Server.URL
	s.reconfigure = Reconfigure{
		BaseReconfigure: BaseReconfigure{
			ConsulAddress: s.ConsulAddress,
			TemplatesPath: s.TemplatesPath,
			ConfigsPath:   s.ConfigsPath,
		},
		ServiceReconfigure: ServiceReconfigure{
			ServiceName: s.ServiceName,
			ServicePath: s.ServicePath,
			PathType:    s.PathType,
		},
	}
	proxy = getProxyMock("")
}

// GetConsulTemplate

func (s ReconfigureTestSuite) Test_GetConsulTemplate_ReturnsFormattedContent() {
	front, back, _ := s.reconfigure.GetConsulTemplate(s.reconfigure.ServiceReconfigure)

	s.Equal(s.ConsulTemplateFe, front)
	s.Equal(s.ConsulTemplateBe, back)
}

func (s ReconfigureTestSuite) Test_GetConsulTemplate_AddsHost() {
	s.ConsulTemplateFe = `
    acl url_myService path_beg path/to/my/service/api path_beg path/to/my/other/service/api
    acl domain_myService hdr_dom(host) -i my-domain.com
    use_backend myService-be if url_myService domain_myService`
	s.reconfigure.ServiceDomain = s.ServiceDomain
	actual, _, _ := s.reconfigure.GetConsulTemplate(s.reconfigure.ServiceReconfigure)

	s.Equal(s.ConsulTemplateFe, actual)
}

func (s ReconfigureTestSuite) Test_GetConsulTemplate_UsesPathReg() {
	s.ConsulTemplateFe = strings.Replace(s.ConsulTemplateFe, "path_beg", "path_reg", -1)
	s.reconfigure.PathType = "path_reg"
	front, _, _ := s.reconfigure.GetConsulTemplate(s.reconfigure.ServiceReconfigure)

	s.Equal(s.ConsulTemplateFe, front)
}

func (s ReconfigureTestSuite) Test_GetConsulTemplate_AddsColor() {
	s.reconfigure.ServiceColor = "black"
	expected := fmt.Sprintf(`service "%s-%s"`, s.ServiceName, s.reconfigure.ServiceColor)

	_, actual, _ := s.reconfigure.GetConsulTemplate(s.reconfigure.ServiceReconfigure)

	s.Contains(actual, expected)
}

func (s ReconfigureTestSuite) Test_GetConsulTemplate_DoesNotSetCheckWhenSkipCheckIsTrue() {
	s.ConsulTemplateBe = strings.Replace(s.ConsulTemplateBe, " check", "", -1)
	s.reconfigure.SkipCheck = true
	_, actual, _ := s.reconfigure.GetConsulTemplate(s.reconfigure.ServiceReconfigure)

	s.Equal(s.ConsulTemplateBe, actual)
}

func (s ReconfigureTestSuite) Test_GetConsulTemplate_ReturnsFileContent_WhenConsulTemplatePathIsSet() {
	expected := "This is content of a template"
	readTemplateFileOrig := readTemplateFile
	defer func() { readTemplateFile = readTemplateFileOrig }()
	readTemplateFile = func(dirname string) ([]byte, error) {
		return []byte(expected), nil
	}
	s.ServiceReconfigure.ConsulTemplateFePath = "/path/to/my/consul/fe/template"
	s.ServiceReconfigure.ConsulTemplateBePath = "/path/to/my/consul/be/template"

	_, actual, _ := s.reconfigure.GetConsulTemplate(s.ServiceReconfigure)

	s.Equal(expected, actual)
}

func (s ReconfigureTestSuite) Test_GetConsulTemplate_ReturnsError_WhenConsulTemplateFileIsNotAvailable() {
	readTemplateFileOrig := readTemplateFile
	defer func() { readTemplateFile = readTemplateFileOrig }()
	readTemplateFile = func(dirname string) ([]byte, error) {
		return nil, fmt.Errorf("This is an error")
	}
	s.ServiceReconfigure.ConsulTemplateFePath = "/path/to/my/consul/fe/template"
	s.ServiceReconfigure.ConsulTemplateBePath = "/path/to/my/consul/be/template"

	_, _, actual := s.reconfigure.GetConsulTemplate(s.ServiceReconfigure)

	s.Error(actual)
}

// Execute

func (s ReconfigureTestSuite) Test_Execute_CreatesConsulTemplate() {
	var actual []string
	writeConsulTemplateFile = func(filename string, data []byte, perm os.FileMode) error {
		actual = append(actual, string(data))
		return nil
	}

	s.reconfigure.Execute([]string{})

	s.Equal(2, len(actual))
	s.Equal(s.ConsulTemplateFe, actual[0])
	s.Equal(s.ConsulTemplateBe, actual[1])
}

func (s ReconfigureTestSuite) Test_Execute_WritesTemplateToFile() {
	var actual []string
	expectedFe := fmt.Sprintf("%s/%s", s.TemplatesPath, ServiceTemplateFeFilename)
	expectedBe := fmt.Sprintf("%s/%s", s.TemplatesPath, ServiceTemplateBeFilename)
	writeConsulTemplateFile = func(filename string, data []byte, perm os.FileMode) error {
		actual = append(actual, filename)
		return nil
	}

	s.reconfigure.Execute([]string{})

	s.Equal(2, len(actual))
	s.Equal(expectedFe, actual[0])
	s.Equal(expectedBe, actual[1])
}

func (s ReconfigureTestSuite) Test_Execute_SetsFilePermissions() {
	var actual os.FileMode
	var expected os.FileMode = 0664
	writeConsulTemplateFile = func(filename string, data []byte, perm os.FileMode) error {
		actual = perm
		return nil
	}

	s.reconfigure.Execute([]string{})

	s.Equal(expected, actual)
}

func (s ReconfigureTestSuite) Test_Execute_RunsConsulTemplate() {
	var actual [][]string
	cmdRunConsul = func(cmd *exec.Cmd) error {
		actual = append(actual, cmd.Args)
		return nil
	}
	expectedFe := []string{
		"consul-template",
		"-consul",
		strings.Replace(s.ConsulAddress, "http://", "", -1),
		"-template",
		fmt.Sprintf(
			`%s/%s:%s/%s-fe.cfg`,
			s.TemplatesPath,
			ServiceTemplateFeFilename,
			s.TemplatesPath,
			s.ServiceName,
		),
		"-once",
	}
	expectedBe := []string{
		"consul-template",
		"-consul",
		strings.Replace(s.ConsulAddress, "http://", "", -1),
		"-template",
		fmt.Sprintf(
			`%s/%s:%s/%s-be.cfg`,
			s.TemplatesPath,
			ServiceTemplateBeFilename,
			s.TemplatesPath,
			s.ServiceName,
		),
		"-once",
	}

	s.reconfigure.Execute([]string{})

	s.Equal(2, len(actual))
	s.Equal(expectedFe, actual[0])
	s.Equal(expectedBe, actual[1])
}

func (s ReconfigureTestSuite) Test_Execute_RunsConsulTemplateWithTrimmedHttp() {
	var actual [][]string
	cmdRunConsul = func(cmd *exec.Cmd) error {
		actual = append(actual, cmd.Args)
		return nil
	}
	expectedFe := []string{
		"consul-template",
		"-consul",
		strings.Replace(s.ConsulAddress, "http://", "", -1),
		"-template",
		fmt.Sprintf(
			`%s/%s:%s/%s-fe.cfg`,
			s.TemplatesPath,
			ServiceTemplateFeFilename,
			s.TemplatesPath,
			s.ServiceName,
		),
		"-once",
	}
	expectedBe := []string{
		"consul-template",
		"-consul",
		strings.Replace(s.ConsulAddress, "http://", "", -1),
		"-template",
		fmt.Sprintf(
			`%s/%s:%s/%s-be.cfg`,
			s.TemplatesPath,
			ServiceTemplateBeFilename,
			s.TemplatesPath,
			s.ServiceName,
		),
		"-once",
	}
	s.reconfigure.ConsulAddress = fmt.Sprintf("HttP://%s", s.ConsulAddress)

	s.reconfigure.Execute([]string{})

	s.Equal(2, len(actual))
	s.Equal(expectedFe, actual[0])
	s.Equal(expectedBe, actual[1])
}

func (s ReconfigureTestSuite) Test_Execute_RunsConsulTemplateWithTrimmedHttps() {
	var actual [][]string
	cmdRunConsul = func(cmd *exec.Cmd) error {
		actual = append(actual, cmd.Args)
		return nil
	}
	expectedFe := []string{
		"consul-template",
		"-consul",
		strings.Replace(s.ConsulAddress, "http://", "", -1),
		"-template",
		fmt.Sprintf(
			`%s/%s:%s/%s-fe.cfg`,
			s.TemplatesPath,
			ServiceTemplateFeFilename,
			s.TemplatesPath,
			s.ServiceName,
		),
		"-once",
	}
	expectedBe := []string{
		"consul-template",
		"-consul",
		strings.Replace(s.ConsulAddress, "http://", "", -1),
		"-template",
		fmt.Sprintf(
			`%s/%s:%s/%s-be.cfg`,
			s.TemplatesPath,
			ServiceTemplateBeFilename,
			s.TemplatesPath,
			s.ServiceName,
		),
		"-once",
	}
	s.reconfigure.ConsulAddress = fmt.Sprintf("HttPs://%s", s.ConsulAddress)
	s.reconfigure.ConsulAddress = s.ConsulAddress

	s.reconfigure.Execute([]string{})

	s.Equal(2, len(actual))
	s.Equal(expectedFe, actual[0])
	s.Equal(expectedBe, actual[1])
}

func (s ReconfigureTestSuite) Test_Execute_ReturnsError_WhenConsulTemplateFeCommandFails() {
	cmdRunConsul = func(cmd *exec.Cmd) error {
		if strings.Contains(cmd.Args[4], "fe.ctmpl") {
			return fmt.Errorf("This is an error")
		}
		return nil
	}

	err := s.reconfigure.Execute([]string{})

	s.Error(err)
}

func (s ReconfigureTestSuite) Test_Execute_ReturnsError_WhenConsulTemplateBeCommandFails() {
	cmdRunConsul = func(cmd *exec.Cmd) error {
		if strings.Contains(cmd.Args[4], "be.ctmpl") {
			return fmt.Errorf("This is an error")
		}
		return nil
	}

	err := s.reconfigure.Execute([]string{})

	s.Error(err)
}

func (s ReconfigureTestSuite) Test_Execute_InvokesProxyCreateConfigFromTemplates() {
	mockObj := getProxyMock("")
	proxy = mockObj

	s.reconfigure.Execute([]string{})

	mockObj.AssertCalled(s.T(), "CreateConfigFromTemplates", s.TemplatesPath, s.ConfigsPath)
}

func (s ReconfigureTestSuite) Test_Execute_ReturnsError_WhenProxyFails() {
	mockObj := getProxyMock("CreateConfigFromTemplates")
	mockObj.On("CreateConfigFromTemplates", mock.Anything, mock.Anything).Return(fmt.Errorf("This is an error"))
	proxy = mockObj

	err := s.reconfigure.Execute([]string{})

	s.Error(err)
}

func (s ReconfigureTestSuite) Test_Execute_InvokesHaProxyReload() {
	proxyOrig := proxy
	defer func() {
		proxy = proxyOrig
	}()
	mock := getProxyMock("")
	proxy = mock

	s.reconfigure.Execute([]string{})

	mock.AssertCalled(s.T(), "Reload")
}

func (s *ReconfigureTestSuite) Test_Execute_PutsDataToConsul() {
	consulTemplateFePath := "test_configs/tmpl/my-service-fe.tmpl"
	consulTemplateBePath := "test_configs/tmpl/my-service-be.tmpl"
	s.SkipCheck = true
	s.reconfigure.SkipCheck = true
	s.reconfigure.ServiceDomain = s.ServiceDomain
	s.reconfigure.ConsulTemplateFePath = consulTemplateFePath
	s.reconfigure.ConsulTemplateBePath = consulTemplateBePath
	s.reconfigure.Execute([]string{})

	type data struct{ key, value, expected string }

	d := []data{
		data{"color", s.ConsulRequestBody.ServiceColor, s.ServiceColor},
		data{"path", strings.Join(s.ConsulRequestBody.ServicePath, ","), strings.Join(s.ServicePath, ",")},
		data{"domain", s.ConsulRequestBody.ServiceDomain, s.ServiceDomain},
		data{"pathType", s.ConsulRequestBody.PathType, s.PathType},
		data{"skipCheck", fmt.Sprintf("%t", s.ConsulRequestBody.SkipCheck), fmt.Sprintf("%t", s.SkipCheck)},
		data{"consulTemplateFePath", s.ConsulRequestBody.ConsulTemplateFePath, consulTemplateFePath},
		data{"consulTemplateBePath", s.ConsulRequestBody.ConsulTemplateBePath, consulTemplateBePath},
	}
	for _, e := range d {
		s.Equal(e.expected, e.value)
	}
}

func (s *ReconfigureTestSuite) Test_Execute_ReturnsError_WhenPutToConsulFails() {
	s.reconfigure.ConsulAddress = "http:///THIS/URL/DOES/NOT/EXIST"
	actual := s.reconfigure.Execute([]string{})

	s.Error(actual)
}

func (s *ReconfigureTestSuite) Test_Execute_AddsHttpIfNotPresentInPutToConsul() {
	s.reconfigure.ConsulAddress = strings.Replace(s.ConsulAddress, "http://", "", -1)
	s.reconfigure.Execute([]string{})

	s.Equal(s.ServiceColor, s.ConsulRequestBody.ServiceColor)
}

func (s *ReconfigureTestSuite) Test_Execute_SendsServicePathToConsul() {
	s.reconfigure.Execute([]string{})

	s.Equal(s.reconfigure.ServiceColor, s.ConsulRequestBody.ServiceColor)
}

func (s ReconfigureTestSuite) Test_Execute_ReturnsError_WhenConsulTemplateFileIsNotAvailable() {
	readTemplateFileOrig := readTemplateFile
	defer func() { readTemplateFile = readTemplateFileOrig }()
	readTemplateFile = func(dirname string) ([]byte, error) {
		return nil, fmt.Errorf("This is an error")
	}
	s.reconfigure.ServiceReconfigure.ConsulTemplateFePath = "/path/to/my/consul/fe/template"
	s.reconfigure.ServiceReconfigure.ConsulTemplateBePath = "/path/to/my/consul/be/template"

	err := s.reconfigure.Execute([]string{})

	s.Error(err)
}

// NewReconfigure

func (s ReconfigureTestSuite) Test_NewReconfigure_AddsBaseAndService() {
	br := BaseReconfigure{ConsulAddress: "myConsulAddress"}
	sr := ServiceReconfigure{ServiceName: "myService"}

	r := NewReconfigure(br, sr)

	actualBr, actualSr := r.GetData()
	s.Equal(br, actualBr)
	s.Equal(sr, actualSr)
}

func (s ReconfigureTestSuite) Test_NewReconfigure_CreatesNewStruct() {
	r1 := NewReconfigure(
		BaseReconfigure{ConsulAddress: "myConsulAddress"},
		ServiceReconfigure{ServiceName: "myService"},
	)
	r2 := NewReconfigure(BaseReconfigure{}, ServiceReconfigure{})

	actualBr1, actualSr1 := r1.GetData()
	actualBr2, actualSr2 := r2.GetData()
	s.NotEqual(actualBr1, actualBr2)
	s.NotEqual(actualSr1, actualSr2)
}

// ReloadAllServices

func (s ReconfigureTestSuite) Test_ReloadAllServices_ReturnsError_WhenFail() {
	err := s.reconfigure.ReloadAllServices("this/address/does/not/exist")

	s.Error(err)
}

func (s ReconfigureTestSuite) Test_ReloadAllServices_WriteTemplateToFile() {
	var actual []string
	expectedFe := fmt.Sprintf("%s/%s", s.TemplatesPath, ServiceTemplateFeFilename)
	expectedBe := fmt.Sprintf("%s/%s", s.TemplatesPath, ServiceTemplateBeFilename)
	writeConsulTemplateFile = func(filename string, data []byte, perm os.FileMode) error {
		actual = append(actual, filename)
		return nil
	}

	s.reconfigure.ReloadAllServices(s.ConsulAddress)

	s.Equal(2, len(actual))
	s.Equal(expectedFe, actual[0])
	s.Equal(expectedBe, actual[1])
}

func (s ReconfigureTestSuite) Test_ReloadAllServices_InvokesProxyCreateConfigFromTemplates() {
	mockObj := getProxyMock("")
	proxy = mockObj

	s.reconfigure.ReloadAllServices(s.ConsulAddress)

	mockObj.AssertCalled(s.T(), "CreateConfigFromTemplates", s.TemplatesPath, s.ConfigsPath)
}

func (s ReconfigureTestSuite) Test_ReloadAllServices_ReturnsError_WhenProxyCreateConfigFromTemplatesFails() {
	mockObj := getProxyMock("CreateConfigFromTemplates")
	mockObj.On("CreateConfigFromTemplates", mock.Anything, mock.Anything).Return(fmt.Errorf("This is an error"))
	proxy = mockObj

	actual := s.reconfigure.ReloadAllServices(s.ConsulAddress)

	s.Error(actual)
}

func (s ReconfigureTestSuite) Test_ReloadAllServices_InvokesProxyReload() {
	mockObj := getProxyMock("")
	proxy = mockObj

	s.reconfigure.ReloadAllServices(s.ConsulAddress)

	mockObj.AssertCalled(s.T(), "Reload")
}

func (s ReconfigureTestSuite) Test_ReloadAllServices_ReturnsError_WhenProxyReloadFails() {
	mockObj := getProxyMock("Reload")
	mockObj.On("Reload").Return(fmt.Errorf("This is an error"))
	proxy = mockObj

	actual := s.reconfigure.ReloadAllServices(s.ConsulAddress)

	s.Error(actual)
}

func (s ReconfigureTestSuite) Test_ReloadAllServices_AddsHttpIfNotPresent() {
	address := strings.Replace(s.ConsulAddress, "http://", "", -1)
	err := s.reconfigure.ReloadAllServices(address)

	s.NoError(err)
}

// Suite

func TestReconfigureTestSuite(t *testing.T) {
	logPrintf = func(format string, v ...interface{}) {}
	s := new(ReconfigureTestSuite)
	s.ServiceName = "myService"
	s.PutPathResponse = "PUT_PATH_OK"
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actualPath := r.URL.Path
		if r.Method == "PUT" {
			defer r.Body.Close()
			body, _ := ioutil.ReadAll(r.Body)
			switch actualPath {
			case fmt.Sprintf("/v1/kv/docker-flow/%s/color", s.ServiceName):
				s.ConsulRequestBody.ServiceColor = string(body)
			case fmt.Sprintf("/v1/kv/docker-flow/%s/path", s.ServiceName):
				s.ConsulRequestBody.ServicePath = strings.Split(string(body), ",")
			case fmt.Sprintf("/v1/kv/docker-flow/%s/domain", s.ServiceName):
				s.ConsulRequestBody.ServiceDomain = string(body)
			case fmt.Sprintf("/v1/kv/docker-flow/%s/pathtype", s.ServiceName):
				s.ConsulRequestBody.PathType = string(body)
			case fmt.Sprintf("/v1/kv/docker-flow/%s/skipcheck", s.ServiceName):
				v, _ := strconv.ParseBool(string(body))
				s.ConsulRequestBody.SkipCheck = v
			case fmt.Sprintf("/v1/kv/docker-flow/%s/consultemplatefepath", s.ServiceName):
				s.ConsulRequestBody.ConsulTemplateFePath = string(body)
			case fmt.Sprintf("/v1/kv/docker-flow/%s/consultemplatebepath", s.ServiceName):
				s.ConsulRequestBody.ConsulTemplateBePath = string(body)
			}
		} else if r.Method == "GET" {
			switch actualPath {
			case "/v1/catalog/services":
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				data := map[string][]string{"service1": []string{}, "service2": []string{}, s.ServiceName: []string{}}
				js, _ := json.Marshal(data)
				w.Write(js)
			case fmt.Sprintf("/v1/kv/docker-flow/%s/%s", s.ServiceName, PATH_KEY):
				if r.URL.RawQuery == "raw" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(strings.Join(s.ServicePath, ",")))
				}
			case fmt.Sprintf("/v1/kv/docker-flow/%s/%s", s.ServiceName, COLOR_KEY):
				if r.URL.RawQuery == "raw" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("orange"))
				}
			case fmt.Sprintf("/v1/kv/docker-flow/%s/%s", s.ServiceName, DOMAIN_KEY):
				if r.URL.RawQuery == "raw" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(s.ServiceDomain))
				}
			case fmt.Sprintf("/v1/kv/docker-flow/%s/%s", s.ServiceName, PATH_TYPE_KEY):
				if r.URL.RawQuery == "raw" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(s.PathType))
				}
			case fmt.Sprintf("/v1/kv/docker-flow/%s/%s", s.ServiceName, SKIP_CHECK_KEY):
				if r.URL.RawQuery == "raw" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(fmt.Sprintf("%t", s.SkipCheck)))
				}
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer s.Server.Close()
	suite.Run(t, s)
}

// Mock

type ReconfigureMock struct {
	mock.Mock
}

func (m *ReconfigureMock) Execute(args []string) error {
	params := m.Called(args)
	return params.Error(0)
}

func (m *ReconfigureMock) GetData() (BaseReconfigure, ServiceReconfigure) {
	m.Called()
	return BaseReconfigure{}, ServiceReconfigure{}
}

func (m *ReconfigureMock) ReloadAllServices(address string) error {
	params := m.Called(address)
	return params.Error(0)
}

func (m *ReconfigureMock) GetConsulTemplate(sr ServiceReconfigure) (front, back string, err error) {
	params := m.Called(sr)
	return params.String(0), params.String(1), params.Error(2)
}

func getReconfigureMock(skipMethod string) *ReconfigureMock {
	mockObj := new(ReconfigureMock)
	if skipMethod != "Execute" {
		mockObj.On("Execute", mock.Anything).Return(nil)
	}
	if skipMethod != "GetData" {
		mockObj.On("GetData", mock.Anything, mock.Anything).Return(nil)
	}
	if skipMethod != "ReloadAllServices" {
		mockObj.On("ReloadAllServices", mock.Anything).Return(nil)
	}
	if skipMethod != "GetConsulTemplate" {
		mockObj.On("GetConsulTemplate", mock.Anything).Return("", "", nil)
	}
	return mockObj
}
