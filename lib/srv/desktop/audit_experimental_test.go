//go:build go1.24 && enablesynctest

/*
 * Teleport
 * Copyright (C) 2025  Gravitational, Inc.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package desktop

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gravitational/teleport/api/types/events"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func TestCombinesEventsBasic(t *testing.T) {
	synctest.Run(func() {
		var mu sync.Mutex
		var emitted []*events.DesktopSharedDirectoryRead

		tracker := readAuditTracker{
			clock:         clockwork.NewRealClock(),
			pendingEvents: make(map[string]*pendingAuditEvent),
			debounce:      1 * time.Second,
			maxDebounce:   5 * time.Second,
			emitFn: func(ctx context.Context, evt events.AuditEvent) {
				mu.Lock()
				defer mu.Unlock()

				emitted = append(emitted, evt.(*events.DesktopSharedDirectoryRead))
			},
		}

		for i := 0; i < 5; i++ {
			tracker.addEvent(&events.DesktopSharedDirectoryRead{
				DirectoryID:   1,
				DirectoryName: "dir",
				Path:          `C:\Windows\logs\teleport.txt`,

				Offset: 42,
				Length: 10,
			})
		}

		time.Sleep(1001 * time.Millisecond) // any value greater than our 1s debounce will do

		mu.Lock()
		require.Len(t, emitted, 1)
		require.Equal(t, uint32(50), emitted[0].Length)
		mu.Unlock()
	})
}

func TestFlush(t *testing.T) {
	synctest.Run(func() {
		var mu sync.Mutex
		var emitted []*events.DesktopSharedDirectoryRead

		tracker := readAuditTracker{
			clock:         clockwork.NewRealClock(),
			pendingEvents: make(map[string]*pendingAuditEvent),
			debounce:      1 * time.Second,
			maxDebounce:   10 * time.Second,
			emitFn: func(ctx context.Context, evt events.AuditEvent) {
				mu.Lock()
				defer mu.Unlock()

				emitted = append(emitted, evt.(*events.DesktopSharedDirectoryRead))
			},
		}

		for i := 0; i < 3; i++ {
			tracker.addEvent(&events.DesktopSharedDirectoryRead{
				DirectoryID:   1,
				DirectoryName: "dir",
				Path:          `C:\Windows\logs\teleport.txt`,

				Offset: 42,
				Length: uint32(10 + i),
			})

			time.Sleep(2 * time.Second)
		}

		tracker.flush()

		mu.Lock()
		// no events should have been combined since they were all 2s apart
		require.Len(t, emitted, 3)
		require.Equal(t, uint32(10), emitted[0].Length)
		require.Equal(t, uint32(11), emitted[1].Length)
		require.Equal(t, uint32(12), emitted[2].Length)
		mu.Unlock()
	})
}

func TestDoesntCombineDifferentPaths(t *testing.T) {
	synctest.Run(func() {
		var mu sync.Mutex
		var emitted []*events.DesktopSharedDirectoryRead

		tracker := readAuditTracker{
			clock:         clockwork.NewRealClock(),
			pendingEvents: make(map[string]*pendingAuditEvent),
			debounce:      1 * time.Second,
			maxDebounce:   5 * time.Second,
			emitFn: func(ctx context.Context, evt events.AuditEvent) {
				mu.Lock()
				defer mu.Unlock()

				emitted = append(emitted, evt.(*events.DesktopSharedDirectoryRead))
			},
		}

		for i := 0; i < 2; i++ {
			tracker.addEvent(&events.DesktopSharedDirectoryRead{
				DirectoryID:   1,
				DirectoryName: "dir",
				Path:          fmt.Sprintf(`C:\Windows\logs\teleport-%v.txt`, i),

				Offset: uint64(i),
				Length: uint32(i + 10),
			})
		}

		time.Sleep(1001 * time.Millisecond) // any value greater than our 1s debounce will do

		mu.Lock()
		require.Len(t, emitted, 2)
		require.ElementsMatch(t, []string{emitted[0].Path, emitted[1].Path}, []string{
			`C:\Windows\logs\teleport-0.txt`,
			`C:\Windows\logs\teleport-1.txt`,
		})
		mu.Unlock()
	})
}

func TestMaxDebounce(t *testing.T) {
	synctest.Run(func() {
		var mu sync.Mutex
		var emitted []*events.DesktopSharedDirectoryRead

		tracker := readAuditTracker{
			clock:         clockwork.NewRealClock(),
			pendingEvents: make(map[string]*pendingAuditEvent),
			debounce:      1 * time.Second,
			maxDebounce:   2 * time.Second,
			emitFn: func(ctx context.Context, evt events.AuditEvent) {
				mu.Lock()
				defer mu.Unlock()

				emitted = append(emitted, evt.(*events.DesktopSharedDirectoryRead))
			},
		}

		for i := 0; i < 4; i++ {
			tracker.addEvent(&events.DesktopSharedDirectoryRead{
				DirectoryID:   1,
				DirectoryName: "dir",
				Path:          `C:\Windows\logs\teleport.txt`,
				Length:        10,
			})
			time.Sleep(999 * time.Millisecond)
		}

		time.Sleep(1 * time.Second)

		mu.Lock()
		// The first 3 events at time 0ms, 999ms, and 1998ms get combined.
		// The 4th event at time 2997ms exceeds max debounce of 2s so wasn't combined.
		require.Len(t, emitted, 2)
		require.Equal(t, uint32(30), emitted[0].Length)
		require.Equal(t, uint32(10), emitted[1].Length)
		mu.Unlock()
	})
}
