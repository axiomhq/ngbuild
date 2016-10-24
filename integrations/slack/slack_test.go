package slack

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/nlopes/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/watchly/ngbuild/core"
	"github.com/watchly/ngbuild/mocks"
)

type slackApi struct {
	lastAttachments []slack.Attachment
	lastError       error
}

func (s *slackApi) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	attachments := r.FormValue("attachments")
	s.lastError = json.Unmarshal([]byte(attachments), &s.lastAttachments)

	w.Write([]byte(`{ "ok": true }`))
}

func TestMain(m *testing.M) {
	silent = true

	os.Exit(m.Run())
}

func TestBasics(t *testing.T) {
	assert := assert.New(t)

	s := Slack{}

	assert.Equal("slack", s.Identifier())
	assert.Error(s.ProvideFor(nil, "foo"))
}

func TestAttachToApp(t *testing.T) {
	assert := assert.New(t)

	s := Slack{
		clientID:     "id",
		clientSecret: "secret",
	}

	app := &mocks.App{}

	call := app.On("Listen", mock.AnythingOfType("string"), mock.Anything)
	call.Return(1)
	call.Run(func(args mock.Arguments) {
		listener := args[0].(string)
		assert.Equal(core.SignalBuildComplete, listener)
	})

	assert.NoError(s.AttachToApp(app))
	app.AssertExpectations(t)
}

func TestSignal(t *testing.T) {
	assert := assert.New(t)

	s := Slack{}
	token := ""

	app := &mocks.App{}

	onBuildCompleteFunc := s.onBuildComplete(app)

	getBuildCall := app.On("GetBuild", mock.AnythingOfType("string"))

	getBuildCall.Return(nil, errors.New("ello love"))
	getBuildCall.Run(func(args mock.Arguments) {
		t := args[0].(string)
		assert.Equal(token, t)
	})
	onBuildCompleteFunc(map[string]string{})
	app.AssertExpectations(t)

	token = "213j1i2j3i1oj3ij13"

	onBuildCompleteFunc(map[string]string{"token": token})

	build := &mocks.Build{}

	exitCodeCall := build.On("ExitCode")

	getBuildCall.Return(build)

	exitCodeCall.Return(0, errors.New("Nope"))
	onBuildCompleteFunc(map[string]string{"token": token})

	// Rest of the tests will actually want to post something to slack

	app.On("Name").Return("ngbuild")

	configCall := app.On("Config", mock.Anything, mock.Anything)
	configCall.Return(nil)

	build.On("Token").Return(token)
	build.On("BuildTime").Return(654 * time.Second)

	api := &slackApi{}
	server := httptest.NewServer(api)

	slack.SLACK_API = server.URL + "/"
	s.setClient("foobarbaz")

	// Successful
	exitCodeCall.Return(0, nil)
	onBuildCompleteFunc(map[string]string{"token": token})
	assert.NoError(api.lastError)
	assert.Len(api.lastAttachments, 1)
	assert.Equal(api.lastAttachments[0].Color, colorSucceeded)
	assert.Contains(api.lastAttachments[0].Title, "ngbuild")
	assert.Len(api.lastAttachments[0].Actions, 0)

	exitCodeCall.Return(1, nil)
	onBuildCompleteFunc(map[string]string{"token": token})
	assert.NoError(api.lastError)
	assert.Len(api.lastAttachments, 1)
	assert.Equal(api.lastAttachments[0].Color, colorFailed)
	assert.Contains(api.lastAttachments[0].Title, "ngbuild")
	assert.Len(api.lastAttachments[0].Actions, 1)
	assert.Equal("rebuild", api.lastAttachments[0].Actions[0].Value)
}

func TestActionCallback(t *testing.T) {
	assert := assert.New(t)

	s := Slack{}

	app := &mocks.App{}
	s.apps = append(s.apps, app)

	handleSlackAction := s.handleSlackAction()

	req := &http.Request{}

	res := httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)
	assert.EqualValues(0, res.Body.Len())

	req.Form.Add("payload", "{")
	res = httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)
	assert.EqualValues(0, res.Body.Len())

	acb := slack.AttachmentActionCallback{}
	acb.User = slack.User{Name: "Stevie Wonder"}

	data, _ := json.Marshal(&acb)
	req.Form.Set("payload", string(data))
	res = httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)
	assert.EqualValues(0, res.Body.Len())

	acb.Actions = []slack.AttachmentAction{
		slack.AttachmentAction{},
	}

	data, _ = json.Marshal(&acb)
	req.Form.Set("payload", string(data))
	res = httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)
	assert.EqualValues(0, res.Body.Len())

	token := "hello"

	acb.CallbackID = token
	acb.Actions[0].Value = "isitmeyourlookingfor"

	data, _ = json.Marshal(&acb)
	req.Form.Set("payload", string(data))
	res = httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)

	acb.OriginalMessage.Attachments = []slack.Attachment{
		slack.Attachment{},
	}
	acb.Actions[0].Value = actionValueRebuild

	getBuildCall := app.On("GetBuild", mock.Anything)
	getBuildCall.Return(nil, errors.New("icanseeitinyoureyes"))
	getBuildCall.Run(func(args mock.Arguments) {
		t := args[0].(string)
		assert.Equal(token, t)
	})

	params := messageParams{}

	data, _ = json.Marshal(&acb)
	req.Form.Set("payload", string(data))
	res = httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)
	assert.NoError(json.Unmarshal(res.Body.Bytes(), &params))
	assert.Len(params.Attachments, 2)
	assert.Contains(params.Attachments[1].Text, "No matching")

	build := &mocks.Build{}

	getBuildCall.Return(build, nil)

	newBuildCall := build.On("NewBuild")
	newBuildCall.Return("", errors.New("icanseeitinyoursmile"))

	res = httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)
	assert.NoError(json.Unmarshal(res.Body.Bytes(), &params))
	assert.Len(params.Attachments, 2)
	assert.Contains(params.Attachments[1].Text, "Unable to start")

	newBuildCall.Return("yourealliveeverwanted", nil)

	res = httptest.NewRecorder()
	handleSlackAction(res, req)
	assert.EqualValues(http.StatusOK, res.Code)
	assert.NoError(json.Unmarshal(res.Body.Bytes(), &params))
	assert.Len(params.Attachments, 2)
	assert.Contains(params.Attachments[1].Text, "requested a rebuild")
	assert.Contains(params.Attachments[1].Text, "Stevie Wonder")
}
