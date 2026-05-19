package dingtalk

import (
	"reflect"
	"testing"
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
	raw := []byte(`{"data":[{"name":"张三","empId":"123456","email":"zhangsan@alibaba-inc.com"}]}`)
	got := extractStaffDingUserIDs(raw)
	want := []string{"zhangsan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractStaffDingUserIDs = %#v; want %#v", got, want)
	}
}
