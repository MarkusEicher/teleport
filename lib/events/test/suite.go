/*
Copyright 2018-2020 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	apidefaults "github.com/gravitational/teleport/api/defaults"
	"github.com/gravitational/teleport/api/types"
	apievents "github.com/gravitational/teleport/api/types/events"
	apiutils "github.com/gravitational/teleport/api/utils"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/fixtures"
	"github.com/gravitational/teleport/lib/session"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

// UploadDownload tests uploads and downloads
func UploadDownload(t *testing.T, handler events.MultipartHandler) {
	val := "hello, how is it going? this is the uploaded file"
	id := session.NewID()
	_, err := handler.Upload(context.TODO(), id, bytes.NewBuffer([]byte(val)))
	require.Nil(t, err)

	f, err := os.CreateTemp("", string(id))
	require.Nil(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	err = handler.Download(context.TODO(), id, f)
	require.Nil(t, err)

	_, err = f.Seek(0, 0)
	require.Nil(t, err)

	data, err := io.ReadAll(f)
	require.Nil(t, err)
	require.Equal(t, string(data), val)
}

// DownloadNotFound tests handling of the scenario when download is not found
func DownloadNotFound(t *testing.T, handler events.MultipartHandler) {
	id := session.NewID()

	f, err := os.CreateTemp("", string(id))
	require.Nil(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	err = handler.Download(context.TODO(), id, f)
	fixtures.AssertNotFound(t, err)
}

// EventsSuite is a conformance test suite to verify external event backends
type EventsSuite struct {
	Log        events.IAuditLog
	Clock      clockwork.Clock
	QueryDelay time.Duration
}

// EventPagination covers event search pagination.
func (s *EventsSuite) EventPagination(t *testing.T) {
	// This serves no special purpose except to make querying easier.
	baseTime := time.Date(2019, time.May, 10, 14, 43, 0, 0, time.UTC)

	names := []string{"bob", "jack", "daisy", "evan"}

	for i, name := range names {
		err := s.Log.EmitAuditEvent(context.Background(), &apievents.UserLogin{
			Method:       events.LoginMethodSAML,
			Status:       apievents.Status{Success: true},
			UserMetadata: apievents.UserMetadata{User: name},
			Metadata: apievents.Metadata{
				Type: events.UserLoginEvent,
				Time: baseTime.Add(time.Second * time.Duration(i)),
			},
		})
		require.NoError(t, err)
	}

	toTime := baseTime.Add(time.Hour)
	var arr []apievents.AuditEvent
	var err error
	var checkpoint string

	err = utils.RetryStaticFor(time.Minute*5, time.Second*5, func() error {
		arr, checkpoint, err = s.Log.SearchEvents(baseTime, toTime, apidefaults.Namespace, nil, 100, types.EventOrderAscending, checkpoint)
		return err
	})
	require.NoError(t, err)
	require.Len(t, arr, 4)
	require.Equal(t, checkpoint, "")

	for _, name := range names {
		arr, checkpoint, err = s.Log.SearchEvents(baseTime, toTime, apidefaults.Namespace, nil, 1, types.EventOrderAscending, checkpoint)
		require.NoError(t, err)
		require.Len(t, arr, 1)
		event, ok := arr[0].(*apievents.UserLogin)
		require.True(t, ok)
		require.Equal(t, name, event.User)
	}
	if checkpoint != "" {
		arr, checkpoint, err = s.Log.SearchEvents(baseTime, toTime, apidefaults.Namespace, nil, 1, types.EventOrderAscending, checkpoint)
		require.NoError(t, err)
		require.Len(t, arr, 0)
	}
	require.Equal(t, checkpoint, "")

	for _, i := range []int{0, 2} {
		nameA := names[i]
		nameB := names[i+1]
		arr, checkpoint, err = s.Log.SearchEvents(baseTime, toTime, apidefaults.Namespace, nil, 2, types.EventOrderAscending, checkpoint)
		require.NoError(t, err)
		require.Len(t, arr, 2)
		eventA, okA := arr[0].(*apievents.UserLogin)
		eventB, okB := arr[1].(*apievents.UserLogin)
		require.True(t, okA)
		require.True(t, okB)
		require.Equal(t, nameA, eventA.User)
		require.Equal(t, nameB, eventB.User)
	}
	if checkpoint != "" {
		arr, checkpoint, err = s.Log.SearchEvents(baseTime, toTime, apidefaults.Namespace, nil, 1, types.EventOrderAscending, checkpoint)
		require.NoError(t, err)
		require.Len(t, arr, 0)
	}
	require.Equal(t, checkpoint, "")

	for i := len(names) - 1; i >= 0; i-- {
		arr, checkpoint, err = s.Log.SearchEvents(baseTime, toTime, apidefaults.Namespace, nil, 1, types.EventOrderDescending, checkpoint)
		require.NoError(t, err)
		require.Len(t, arr, 1)
		event, ok := arr[0].(*apievents.UserLogin)
		require.True(t, ok)
		require.Equal(t, names[i], event.User)
	}
	if checkpoint != "" {
		arr, checkpoint, err = s.Log.SearchEvents(baseTime, toTime, apidefaults.Namespace, nil, 1, types.EventOrderDescending, checkpoint)
		require.NoError(t, err)
		require.Len(t, arr, 0)
	}
	require.Equal(t, checkpoint, "")

	// This serves no special purpose except to make querying easier.
	baseTime2 := time.Date(2019, time.August, 10, 14, 43, 47, 0, time.UTC)

	for _, name := range names {
		err := s.Log.EmitAuditEvent(context.Background(), &apievents.UserLogin{
			Method:       events.LoginMethodSAML,
			Status:       apievents.Status{Success: true},
			UserMetadata: apievents.UserMetadata{User: name},
			Metadata: apievents.Metadata{
				Type: events.UserLoginEvent,
				Time: baseTime2,
			},
		})
		require.NoError(t, err)
	}

Outer:
	for i := 0; i < len(names); i++ {
		arr, checkpoint, err = s.Log.SearchEvents(baseTime2, baseTime2.Add(time.Second), apidefaults.Namespace, nil, 1, types.EventOrderAscending, checkpoint)
		require.NoError(t, err)
		require.Len(t, arr, 1)
		event, ok := arr[0].(*apievents.UserLogin)
		require.True(t, ok)
		require.Equal(t, event.GetTime(), baseTime2)
		require.True(t, apiutils.SliceContainsStr(names, event.User))

		for i, name := range names {
			if name == event.User {
				// delete name from list
				copy(names[i:], names[i+1:])
				names = names[:len(names)-1]
				continue Outer
			}
		}

		t.Fatalf("unexpected event: %#v", event)
	}
}

// SessionEventsCRUD covers session events
func (s *EventsSuite) SessionEventsCRUD(t *testing.T) {
	// Bob has logged in
	err := s.Log.EmitAuditEvent(context.Background(), &apievents.UserLogin{
		Method:       events.LoginMethodSAML,
		Status:       apievents.Status{Success: true},
		UserMetadata: apievents.UserMetadata{User: "bob"},
		Metadata: apievents.Metadata{
			Type: events.UserLoginEvent,
			Time: s.Clock.Now().UTC(),
		},
	})
	require.NoError(t, err)

	// For eventually consistent queries
	if s.QueryDelay != 0 {
		time.Sleep(s.QueryDelay)
	}

	var history []apievents.AuditEvent

	err = utils.RetryStaticFor(time.Minute*5, time.Second*5, func() error {
		history, _, err = s.Log.SearchEvents(s.Clock.Now().Add(-1*time.Hour), s.Clock.Now().Add(time.Hour), apidefaults.Namespace, nil, 100, types.EventOrderAscending, "")
		return err
	})
	require.NoError(t, err)
	require.Len(t, history, 1)

	// start the session and emit data stream to it and wrap it up
	sessionID := session.NewID()

	err = s.Log.EmitAuditEvent(context.Background(), &apievents.SessionStart{
		Metadata: apievents.Metadata{
			Time:  s.Clock.Now().UTC(),
			Index: 0,
			Type:  events.SessionStartEvent,
		},
		SessionMetadata: apievents.SessionMetadata{
			SessionID: string(sessionID),
		},
		UserMetadata: apievents.UserMetadata{
			Login: "bob",
		},
	})
	require.NoError(t, err)

	err = s.Log.EmitAuditEvent(context.Background(), &apievents.SessionEnd{
		Metadata: apievents.Metadata{
			Time:  s.Clock.Now().Add(time.Hour).UTC(),
			Index: 4,
			Type:  events.SessionEndEvent,
		},
		UserMetadata: apievents.UserMetadata{
			Login: "bob",
		},
		SessionMetadata: apievents.SessionMetadata{
			SessionID: string(sessionID),
		},
		Participants: []string{"bob", "alice"},
	})
	require.NoError(t, err)

	// read the session event
	historyEvents, err := s.Log.GetSessionEvents(apidefaults.Namespace, sessionID, 0, false)
	require.NoError(t, err)
	require.Len(t, historyEvents, 2)
	require.Equal(t, historyEvents[0].GetString(events.EventType), events.SessionStartEvent)
	require.Equal(t, historyEvents[1].GetString(events.EventType), events.SessionEndEvent)

	history, _, err = s.Log.SearchSessionEvents(s.Clock.Now().Add(-1*time.Hour), s.Clock.Now().Add(2*time.Hour), 100, types.EventOrderAscending, "", nil, "")
	require.NoError(t, err)
	require.Len(t, history, 1)

	withParticipant := func(participant string) *types.WhereExpr {
		return &types.WhereExpr{Contains: types.WhereExpr2{
			L: &types.WhereExpr{Field: events.SessionParticipants},
			R: &types.WhereExpr{Literal: participant},
		}}
	}

	history, _, err = s.Log.SearchSessionEvents(s.Clock.Now().Add(-1*time.Hour), s.Clock.Now().Add(2*time.Hour), 100, types.EventOrderAscending, "", withParticipant("alice"), "")
	require.NoError(t, err)
	require.Len(t, history, 1)

	history, _, err = s.Log.SearchSessionEvents(s.Clock.Now().Add(-1*time.Hour), s.Clock.Now().Add(2*time.Hour), 100, types.EventOrderAscending, "", withParticipant("cecile"), "")
	require.NoError(t, err)
	require.Len(t, history, 0)

	history, _, err = s.Log.SearchSessionEvents(s.Clock.Now().Add(-1*time.Hour), s.Clock.Now().Add(time.Hour-time.Second), 100, types.EventOrderAscending, "", nil, "")
	require.NoError(t, err)
	require.Len(t, history, 0)
}

func (s *EventsSuite) SearchSessionEvensBySessionID(t *testing.T) {
	now := time.Now().UTC()
	firstID := uuid.New().String()
	secondID := uuid.New().String()
	thirdID := uuid.New().String()
	for i, id := range []string{firstID, secondID, thirdID} {
		event := &apievents.WindowsDesktopSessionEnd{
			Metadata: apievents.Metadata{
				ID:   fmt.Sprintf("eventID%d", i),
				Type: events.WindowsDesktopSessionEndEvent,
				Code: events.DesktopSessionEndCode,
				Time: now.Add(time.Duration(i) * time.Second),
			},
			SessionMetadata: apievents.SessionMetadata{
				SessionID: id,
			},
		}
		err := s.Log.EmitAuditEvent(context.Background(), event)
		require.NoError(t, err)
	}
	from := time.Time{}
	to := now.Add(10 * time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		events, _, err := s.Log.SearchSessionEvents(from, to, 1000, types.EventOrderDescending, "", nil, secondID)
		require.NoError(t, err)
		require.Len(t, events, 1)
		e, ok := events[0].(*apievents.WindowsDesktopSessionEnd)
		require.True(t, ok)
		require.Equal(t, e.GetSessionID(), secondID)
	}()

	select {
	case <-time.After(time.Second * 10):
		t.Fatalf("Search event query timeout")
	case <-done:
	}
}
