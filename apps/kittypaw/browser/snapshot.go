package browser

import "strconv"

type SnapshotResult struct {
	TargetID string            `json:"target_id"`
	URL      string            `json:"url"`
	Title    string            `json:"title"`
	Text     string            `json:"text"`
	Elements []snapshotElement `json:"elements"`
}

type snapshotElement struct {
	Ref      string `json:"ref"`
	Role     string `json:"role"`
	Text     string `json:"text,omitempty"`
	Selector string `json:"selector"`
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if limit <= 0 || len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "...(truncated)"
}

func assignRefs(elements []snapshotElement) {
	for i := range elements {
		elements[i].Ref = "e" + strconv.Itoa(i+1)
	}
}

const snapshotScript = `(() => {
  const cssPath = (el) => {
    if (el.id) return "#" + CSS.escape(el.id);
    const parts = [];
    for (let node = el; node && node.nodeType === Node.ELEMENT_NODE && node !== document.body; node = node.parentElement) {
      let part = node.nodeName.toLowerCase();
      if (node.classList && node.classList.length > 0) {
        part += "." + Array.from(node.classList).slice(0, 2).map(c => CSS.escape(c)).join(".");
      }
      const parent = node.parentElement;
      if (parent) {
        const siblings = Array.from(parent.children).filter(child => child.nodeName === node.nodeName);
        if (siblings.length > 1) {
          part += ":nth-of-type(" + (siblings.indexOf(node) + 1) + ")";
        }
      }
      parts.unshift(part);
    }
    return parts.length ? parts.join(" > ") : el.tagName.toLowerCase();
  };
  const roleOf = (el) => {
    if (el.getAttribute("role")) return el.getAttribute("role");
    const tag = el.tagName.toLowerCase();
    if (tag === "a") return "link";
    if (tag === "button") return "button";
    if (tag === "input" || tag === "textarea") return "textbox";
    if (tag === "select") return "select";
    return "element";
  };
  const nodes = Array.from(document.querySelectorAll("a,button,input,textarea,select,[role=button],[onclick]")).slice(0, 80);
  const elements = nodes.map(el => ({
    role: roleOf(el),
    text: (el.innerText || el.value || el.getAttribute("aria-label") || el.getAttribute("placeholder") || "").trim().slice(0, 200),
    selector: cssPath(el)
  }));
  return {
    url: location.href,
    title: document.title,
    text: (document.body && document.body.innerText || "").trim(),
    elements
  };
})()`
