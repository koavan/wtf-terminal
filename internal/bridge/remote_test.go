package bridge

import (
	"fmt"
	"testing"
)

func TestMirror(t *testing.T) {
	s := NewService(nil)
	var got []string
	fwd := func(method, approval string, args ...any) error {
		got = append(got, fmt.Sprint(method, " ", approval, " ", args))
		return nil
	}
	session := RemotePrefix + "box:|r1"
	Mirror(s, session, Status{Session: "r1", State: StateWaiting, Approvals: []Approval{{ID: "a3", What: "ls"}}}, fwd)
	list := s.Agents()
	if len(list) != 1 || list[0].Session != session || list[0].Approvals[0].ID != session+"/a3" {
		t.Fatalf("mirrored: %+v", list)
	}
	if err := s.Answer(session+"/a3", Allow); err != nil {
		t.Fatal(err)
	}
	s.AllowAlways(session+"/a3", 1)
	s.AnswerQuestion(session+"/a3", []string{"Redis"})
	s.DenyWith(session+"/a3", "use make test")
	if want := []string{"Answer a3 [allow]", "AllowAlways a3 [1]", "AnswerQuestion a3 [[Redis]]", "DenyWith a3 [use make test]"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("forwarded %q", got)
	}
	// A local approval id isn't forwarded.
	if err := s.Answer("a3", Allow); err == nil {
		t.Fatal("answered an approval that isn't there")
	}
	if len(s.AgentsIn("/")) != 0 {
		t.Fatal("a runner's folder counts here")
	}
	Unmirror(s, session)
	if len(s.Agents()) != 0 {
		t.Fatal("still listed")
	}
}
