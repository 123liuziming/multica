package dingtalk

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestExtractAoneID(t *testing.T) {
	if got := extractAoneID("[AONE-82226317] Update filters"); got != "82226317" {
		t.Fatalf("extractAoneID = %q", got)
	}
	if got := extractAoneID("no linked item"); got != "" {
		t.Fatalf("extractAoneID without marker = %q", got)
	}
}

func TestParseAoneMirrorInfo(t *testing.T) {
	raw := []byte(`{
		"id":"82226317",
		"fields":[
			{"identifier":"assignedTo","label":"指派给(assignedTo)","value":"123456@alibaba-inc.com"},
			{"identifier":"136","label":"  备注(136)","value":"群聊: cidabc123\nopenConversationId=cidxyz789"}
		]
	}`)
	info, err := parseAoneMirrorInfo(raw)
	if err != nil {
		t.Fatalf("parseAoneMirrorInfo: %v", err)
	}
	if info.Assignee != "123456@alibaba-inc.com" {
		t.Fatalf("Assignee = %q", info.Assignee)
	}
	if len(info.Remarks) != 1 || info.Remarks[0] == "" {
		t.Fatalf("Remarks = %#v", info.Remarks)
	}
}

func TestExtractDingTalkGroupIDs(t *testing.T) {
	got := extractDingTalkGroupIDs("群聊: cidabc123\nopenConversationId=cidxyz789\n[dingtalk:g:cidgroup456]")
	want := []string{"cidabc123", "cidgroup456", "cidxyz789"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractDingTalkGroupIDs = %#v; want %#v", got, want)
	}
}

func TestExtractStaffDingUserIDs(t *testing.T) {
	t.Run("returns empId not email local part", func(t *testing.T) {
		raw := []byte(`{"data":[{"nickname":"张三","employeeId":"123456","email":"zhangsan@alibaba-inc.com"}]}`)
		got := extractStaffDingUserIDs(raw, "")
		want := []string{"123456"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v; want %#v", got, want)
		}
	})

	t.Run("ignores non-numeric staff fields", func(t *testing.T) {
		raw := []byte(`{"data":[{"nickname":"TestUser","employeeId":"john.doe","workNo":"567890"}]}`)
		got := extractStaffDingUserIDs(raw, "")
		want := []string{"567890"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v; want %#v", got, want)
		}
	})

	t.Run("exact nickname match only", func(t *testing.T) {
		raw := []byte(`[{"nickname":"Alice","employeeId":"100001"},{"nickname":"AliceLi","employeeId":"100002"}]`)
		got := extractStaffDingUserIDs(raw, "Alice")
		want := []string{"100001"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v; want %#v", got, want)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		raw := []byte(`[{"nickname":"AliceLi","employeeId":"100002"}]`)
		got := extractStaffDingUserIDs(raw, "Alice")
		if len(got) != 0 {
			t.Fatalf("got %#v; want empty", got)
		}
	})
}

func clearAoneAssigneeCache() {
	aoneAssigneeCache.Lock()
	defer aoneAssigneeCache.Unlock()
	aoneAssigneeCache.entries = make(map[string]aoneAssigneeCacheEntry)
}

func TestResolveSummaryStaffID(t *testing.T) {
	t.Cleanup(clearAoneAssigneeCache)

	t.Run("no aone id returns fallback", func(t *testing.T) {
		got := resolveSummaryStaffID(context.Background(), "plain title", "1001")
		if got != "1001" {
			t.Fatalf("got %q; want 1001", got)
		}
	})

	t.Run("no aone id with empty fallback returns empty", func(t *testing.T) {
		got := resolveSummaryStaffID(context.Background(), "plain title", "")
		if got != "" {
			t.Fatalf("got %q; want empty", got)
		}
	})

	t.Run("cache hit returns assignee", func(t *testing.T) {
		storeAoneAssigneeCache("99999", []string{"5001"})
		got := resolveSummaryStaffID(context.Background(), "[AONE-99999] cached issue", "1001")
		if got != "5001" {
			t.Fatalf("got %q; want 5001", got)
		}
	})

	t.Run("cache hit with multiple returns first", func(t *testing.T) {
		storeAoneAssigneeCache("88888", []string{"5001", "5002"})
		got := resolveSummaryStaffID(context.Background(), "[AONE-88888] multi assign", "1001")
		if got != "5001" {
			t.Fatalf("got %q; want 5001", got)
		}
	})

	t.Run("empty result is not cached", func(t *testing.T) {
		storeAoneAssigneeCache("77777", nil)
		_, found := lookupAoneAssigneeCache("77777")
		if found {
			t.Fatal("empty staffIDs should not be cached")
		}
	})
}

func TestAoneAssigneeCacheHitAndExpiry(t *testing.T) {
	t.Cleanup(clearAoneAssigneeCache)

	storeAoneAssigneeCache("11111", []string{"5001", "5002"})

	ids, found := lookupAoneAssigneeCache("11111")
	if !found || !reflect.DeepEqual(ids, []string{"5001", "5002"}) {
		t.Fatalf("fresh cache: found=%v ids=%v", found, ids)
	}

	ids, found = lookupAoneAssigneeCache("00000")
	if found || ids != nil {
		t.Fatalf("miss should return nil,false; got found=%v ids=%v", found, ids)
	}

	aoneAssigneeCache.Lock()
	aoneAssigneeCache.entries["11111"] = aoneAssigneeCacheEntry{
		staffIDs: []string{"5001"},
		ts:       time.Now().Add(-2 * time.Hour),
	}
	aoneAssigneeCache.Unlock()

	ids, found = lookupAoneAssigneeCache("11111")
	if found || ids != nil {
		t.Fatalf("expired cache should miss; got found=%v ids=%v", found, ids)
	}
}
