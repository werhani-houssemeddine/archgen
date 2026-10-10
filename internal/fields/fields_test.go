package fields

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	fs, err := Parse("title:string:unique, summary:text:optional, level:enum(beginner|advanced):index, price:float, startsAt:time:optional, ownerId:uuid")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 6 {
		t.Fatalf("got %d fields", len(fs))
	}
	if !fs[0].Unique || fs[0].SQLColumn() != "title text NOT NULL" || fs[0].ValidateLine("p.Title") != `v.String("title", p.Title).Required().Max(255)` {
		t.Fatalf("title: %+v %s", fs[0], fs[0].ValidateLine("p.Title"))
	}
	if fs[1].GoType() != "*string" || fs[1].SQLColumn() != "summary text" {
		t.Fatalf("summary: %s %s", fs[1].GoType(), fs[1].SQLColumn())
	}
	if !strings.Contains(fs[2].SQLColumn(), "CHECK (level IN ('beginner', 'advanced'))") || !fs[2].Index {
		t.Fatalf("enum: %s", fs[2].SQLColumn())
	}
	if fs[4].GoType() != "*time.Time" || fs[5].Name != "OwnerID" || fs[5].JSON != "ownerId" || fs[5].Snake != "owner_id" {
		t.Fatalf("names: %+v %+v", fs[4], fs[5])
	}
}

func TestParseErrors(t *testing.T) {
	for _, spec := range []string{"id:uuid", "title", "x:money", "a:string,a:int", "s:enum(a)", "flag:bool:optional", "t:string:weird", "type:string"} {
		if _, err := Parse(spec); err == nil {
			t.Errorf("Parse(%q) should fail", spec)
		}
	}
}
