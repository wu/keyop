package macosNotes

import (
	"keyop/core"
	"strings"
	"testing"
)

func TestParseNotes(t *testing.T) {
	input := `<div><h1>Example Structured Doc</h1></div>
<div><br></div>
<div><h2>links</h2></div>
<div><br></div>
<ul>
<li>point 1</li>
<li>point 2</li>
<ul>
<li>point 2a</li>
<li>point 2b</li>
</ul>
<li>point3</li>
</ul>
<div><br></div>
<div><br></div>
<div><h2>tasks</h2></div>
<div><br></div>
<ul>
<li>point4</li>
<ul>
<li>point4a</li>
</ul>
<li>point5</li>
<li>point6</li>
<li>❌❌❌❌❌❌❌❌❌❌❌❌❌<br></li>
<li>point7</li>
<li>point8</li>
</ul>
<div><br></div>
<div><h2>Journal</h2></div>
<div><br></div>
<ul>
<li>point9</li>
<ul>
<li>point9a</li>
<li>point9b</li>
</ul>
</ul>`

	expected := `todo:
  - point4
    - point4a
  - point5
  - point6

done:
  - point7
  - point8`

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps}
	result := svc.parseNotes(input)
	if strings.TrimSpace(result) != strings.TrimSpace(expected) {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}
