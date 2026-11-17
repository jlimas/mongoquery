package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// mongoStub speaks just enough of the MongoDB wire protocol to exercise the real
// driver and command handlers. It is not a database: query semantics belong in
// integration tests against MongoDB, not in this fixture.
type mongoStub struct {
	t         *testing.T
	listener  net.Listener
	mu        sync.Mutex
	commands  []string
	failCount bool
	failMore  bool
}

func newMongoStub(t *testing.T, failCount bool, failMore ...bool) *mongoStub {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &mongoStub{t: t, listener: listener, failCount: failCount}
	if len(failMore) > 0 {
		s.failMore = failMore[0]
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { conn.Close() })
			go s.serve(conn)
		}
	}()
	config := filepath.Join(t.TempDir(), "config.yaml")
	data := fmt.Sprintf("environments:\n  test:\n    services:\n      app:\n        uri: mongodb://%s/db?directConnection=true&retryReads=false&serverSelectionTimeoutMS=2000\n", listener.Addr())
	if err := os.WriteFile(config, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONGOQUERY_CONFIG", config)
	return s
}

func (s *mongoStub) serve(conn net.Conn) {
	defer conn.Close()
	for {
		header := make([]byte, 16)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		length := int(binary.LittleEndian.Uint32(header))
		if length < 16 || length > 1<<20 {
			return
		}
		body := make([]byte, length-16)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		opcode := binary.LittleEndian.Uint32(header[12:])
		var raw bson.Raw
		switch opcode {
		case 2004: // OP_QUERY handshake: flags, namespace, skip, return, document.
			if len(body) < 4 {
				return
			}
			end := strings.IndexByte(string(body[4:]), 0)
			offset := 4 + end + 1 + 8
			if end < 0 || offset >= len(body) {
				return
			}
			raw = bson.Raw(body[offset:])
		case 2013: // OP_MSG: flags, section kind 0, document.
			if len(body) < 5 || body[4] != 0 {
				return
			}
			raw = bson.Raw(body[5:])
		default:
			return
		}
		elements, err := raw.Elements()
		if err != nil || len(elements) == 0 {
			return
		}
		command := elements[0].Key()
		response := bson.D{{Key: "ok", Value: 1}}
		switch command {
		case "isMaster", "ismaster", "hello":
			response = append(response,
				bson.E{Key: "isWritablePrimary", Value: true},
				bson.E{Key: "minWireVersion", Value: 0},
				bson.E{Key: "maxWireVersion", Value: 25},
				bson.E{Key: "maxBsonObjectSize", Value: 16777216},
				bson.E{Key: "maxMessageSizeBytes", Value: 48000000},
				bson.E{Key: "maxWriteBatchSize", Value: 100000},
			)
		case "find", "aggregate":
			s.mu.Lock()
			s.commands = append(s.commands, command)
			s.mu.Unlock()
			if command == "aggregate" && s.failCount {
				response = bson.D{{Key: "ok", Value: 0}, {Key: "code", Value: 50}, {Key: "errmsg", Value: "count exceeded time limit"}}
			} else {
				doc := bson.D{{Key: "name", Value: "Ada"}}
				if command == "aggregate" {
					doc = bson.D{{Key: "n", Value: int32(42)}}
				}
				var cursorID int64
				if s.failMore {
					cursorID = 123
				}
				response = append(response, bson.E{Key: "cursor", Value: bson.D{
					{Key: "id", Value: cursorID}, {Key: "ns", Value: "db.users"},
					{Key: "firstBatch", Value: bson.A{doc}},
				}})
			}
		case "getMore":
			response = bson.D{{Key: "ok", Value: 0}, {Key: "code", Value: 50}, {Key: "errmsg", Value: "getMore exceeded time limit"}}
		case "killCursors":
			response = append(response, bson.E{Key: "cursorsKilled", Value: bson.A{int64(123)}})
		}
		payload, err := bson.Marshal(response)
		if err != nil {
			s.t.Errorf("marshal stub response: %v", err)
			return
		}
		replyOpcode := uint32(2013)
		prefix := make([]byte, 5)
		if opcode == 2004 {
			replyOpcode = 1 // OP_REPLY
			prefix = make([]byte, 20)
			binary.LittleEndian.PutUint32(prefix[16:], 1)
		}
		reply := make([]byte, 16)
		binary.LittleEndian.PutUint32(reply, uint32(16+len(prefix)+len(payload)))
		copy(reply[8:12], header[4:8])
		binary.LittleEndian.PutUint32(reply[12:], replyOpcode)
		reply = append(append(reply, prefix...), payload...)
		if _, err := conn.Write(reply); err != nil {
			return
		}
	}
}

func TestAggregationDoesNotReportPartialResultsOnGetMoreFailure(t *testing.T) {
	newMongoStub(t, false, true)
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			out, stderr, err := captureCommand(t, func() error {
				return cmdAggregate(context.Background(), []string{
					"--env", "test", "--service", "app", "--collection", "users",
					"--pipeline", "[]", "--format", format,
				})
			})
			if err == nil || out != "" || !strings.Contains(stderr, "getMore exceeded time limit") {
				t.Fatalf("partial result or swallowed failure: stdout=%q stderr=%q err=%v", out, stderr, err)
			}
		})
	}
}

func TestFindCountIsOptIn(t *testing.T) {
	for _, tt := range []struct {
		name       string
		count      bool
		failCount  bool
		wantHeader string
		wantCalls  []string
	}{
		{"no count", false, false, "results[1]:", []string{"find"}},
		{"exact count", true, false, "results[1 of 42 total]:", []string{"find", "aggregate"}},
		{"count failure", true, true, "error: count failed:", []string{"find", "aggregate"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := newMongoStub(t, tt.failCount)
			args := []string{"--env", "test", "--service", "app", "--collection", "users", "--limit", "1"}
			if tt.count {
				args = append(args, "--count")
			}
			stdout, stderr, err := captureCommand(t, func() error {
				return cmdFind(context.Background(), args)
			})
			if (err != nil) != tt.failCount {
				t.Fatalf("unexpected command error: %v", err)
			}
			data := stdout
			if tt.failCount {
				data = stderr
			}
			if !strings.Contains(data, tt.wantHeader) {
				t.Fatalf("missing %q in %s", tt.wantHeader, data)
			}
			if tt.failCount && stdout != "" {
				t.Fatalf("count failure wrote to stdout: %s", stdout)
			}
			server.mu.Lock()
			calls := append([]string(nil), server.commands...)
			server.mu.Unlock()
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("database commands: got %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}
