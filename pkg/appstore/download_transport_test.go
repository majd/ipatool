package appstore

import (
	"errors"
	"fmt"
	"net/http"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
)

// File/download fixtures use local HTTP servers. Store API calls in these tests
// use mocks; this executor deliberately cannot send a Store request.
type downloadTestExecutor struct{}

func (downloadTestExecutor) Send(apphttp.Request, []byte) (*http.Response, error) {
	return nil, errors.New("unexpected Store request in download fixture")
}

func (downloadTestExecutor) Do(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download fixture: %w", err)
	}

	return response, nil
}

func newDownloadTestClient() apphttp.Client[interface{}] {
	return apphttp.NewClient[interface{}](apphttp.Args{Executor: downloadTestExecutor{}})
}
