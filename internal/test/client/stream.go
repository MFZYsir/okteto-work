// Copyright 2023 The Okteto Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
)

// FakeStreamClient mocks the stream client interface
type FakeStreamClient struct {
	response *FakeStreamResponse
}

// FakeStreamResponse mocks the stream response
type FakeStreamResponse struct {
	StreamErr error
}

// NewFakeStreamClient returns a new fake stream client
func NewFakeStreamClient(response *FakeStreamResponse) *FakeStreamClient {
	return &FakeStreamClient{
		response: response,
	}
}

// PipelineLogs starts the streaming of pipeline logs
func (c *FakeStreamClient) PipelineLogs(_ context.Context, _, _, _ string) error {
	return c.response.StreamErr
}

// DestroyAllLogs starts the streaming of pipeline logs
func (c *FakeStreamClient) DestroyAllLogs(_ context.Context, _ string) error {
	return c.response.StreamErr
}
