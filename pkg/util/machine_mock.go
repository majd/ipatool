// Code generated by MockGen. DO NOT EDIT.
// Source: machine.go

// Package util is a generated GoMock package.
package util

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockMachine is a mock of Machine interface.
type MockMachine struct {
	ctrl     *gomock.Controller
	recorder *MockMachineMockRecorder
}

// MockMachineMockRecorder is the mock recorder for MockMachine.
type MockMachineMockRecorder struct {
	mock *MockMachine
}

// NewMockMachine creates a new mock instance.
func NewMockMachine(ctrl *gomock.Controller) *MockMachine {
	mock := &MockMachine{ctrl: ctrl}
	mock.recorder = &MockMachineMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMachine) EXPECT() *MockMachineMockRecorder {
	return m.recorder
}

// HomeDirectory mocks base method.
func (m *MockMachine) HomeDirectory() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HomeDirectory")
	ret0, _ := ret[0].(string)
	return ret0
}

// HomeDirectory indicates an expected call of HomeDirectory.
func (mr *MockMachineMockRecorder) HomeDirectory() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HomeDirectory", reflect.TypeOf((*MockMachine)(nil).HomeDirectory))
}

// MacAddress mocks base method.
func (m *MockMachine) MacAddress() (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MacAddress")
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// MacAddress indicates an expected call of MacAddress.
func (mr *MockMachineMockRecorder) MacAddress() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MacAddress", reflect.TypeOf((*MockMachine)(nil).MacAddress))
}

// ReadPassword mocks base method.
func (m *MockMachine) ReadPassword(fd int) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReadPassword", fd)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ReadPassword indicates an expected call of ReadPassword.
func (mr *MockMachineMockRecorder) ReadPassword(fd interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReadPassword", reflect.TypeOf((*MockMachine)(nil).ReadPassword), fd)
}
