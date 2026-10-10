package naming

import "testing"

func TestCases(t *testing.T) {
	cases := []struct{ in, pascal, camel, snake, json, pkg string }{
		{"user_id", "UserID", "userID", "user_id", "userId", "userid"},
		{"UserProfile", "UserProfile", "userProfile", "user_profile", "userProfile", "userprofile"},
		{"course-sessions", "CourseSessions", "courseSessions", "course_sessions", "courseSessions", "coursesessions"},
		{"HTTPServer", "HTTPServer", "httpServer", "http_server", "httpServer", "httpserver"},
		{"ownerId", "OwnerID", "ownerID", "owner_id", "ownerId", "ownerid"},
	}
	for _, c := range cases {
		if got := Pascal(c.in); got != c.pascal {
			t.Errorf("Pascal(%q)=%q want %q", c.in, got, c.pascal)
		}
		if got := Camel(c.in); got != c.camel {
			t.Errorf("Camel(%q)=%q want %q", c.in, got, c.camel)
		}
		if got := Snake(c.in); got != c.snake {
			t.Errorf("Snake(%q)=%q want %q", c.in, got, c.snake)
		}
		if got := JSONName(c.in); got != c.json {
			t.Errorf("JSONName(%q)=%q want %q", c.in, got, c.json)
		}
		if got := Package(c.in); got != c.pkg {
			t.Errorf("Package(%q)=%q want %q", c.in, got, c.pkg)
		}
	}
}

func TestInflection(t *testing.T) {
	for sg, pl := range map[string]string{"Course": "Courses", "Category": "Categories", "Class": "Classes", "Person": "People",
		"OrderItem": "OrderItems", "Status": "Statuses", "Day": "Days", "Box": "Boxes", "Leaf": "Leaves", "Quiz": "Quizzes"} {
		if got := Plural(sg); got != pl {
			t.Errorf("Plural(%q)=%q want %q", sg, got, pl)
		}
		if got := Singular(pl); got != sg {
			t.Errorf("Singular(%q)=%q want %q", pl, got, sg)
		}
	}
}
