"""Compute parallel waves from a ChangeGraph YAML.

Usage: uv run --with pyyaml python waves.py <changegraph.yaml>
       uv run --with pyyaml python waves.py --selftest

Rules:
  1. Nodes sharing any file never run in the same wave.
  2. A node runs only after every depends_on node is done (earlier wave or done: true).
"""

import sys


def waves(nodes):
    by_id = {n["id"]: n for n in nodes}
    if len(by_id) != len(nodes):
        raise ValueError("duplicate node id")
    for n in nodes:
        for d in n.get("depends_on", []):
            if d not in by_id:
                raise ValueError(f"{n['id']}: unknown depends_on {d}")
    finished = {n["id"] for n in nodes if n.get("done")}
    todo = [n for n in nodes if not n.get("done")]
    result = []
    while todo:
        wave, used = [], set()
        for n in todo:  # file order = priority
            files = set(n.get("files", []))
            if set(n.get("depends_on", [])) <= finished and not files & used:
                wave.append(n["id"])
                used |= files
        if not wave:
            raise ValueError(f"cycle or unsatisfiable: {[n['id'] for n in todo]}")
        result.append(wave)
        finished |= set(wave)
        todo = [n for n in todo if n["id"] not in wave]
    return result


def overlaps(nodes):
    out = []
    for i, a in enumerate(nodes):
        for b in nodes[i + 1 :]:
            shared = set(a.get("files", [])) & set(b.get("files", []))
            if shared:
                out.append((a["id"], b["id"], sorted(shared)))
    return out


def selftest():
    g = [
        {"id": "a", "files": ["x.py"]},
        {"id": "b", "files": ["x.py", "y.py"]},
        {"id": "c", "files": ["z.py"], "depends_on": ["a"]},
        {"id": "d", "files": ["w.py"], "done": True},
    ]
    assert waves(g) == [["a"], ["b", "c"]], waves(g)
    assert overlaps(g) == [("a", "b", ["x.py"])]
    try:
        waves([{"id": "p", "depends_on": ["q"]}, {"id": "q", "depends_on": ["p"]}])
        raise AssertionError("cycle not detected")
    except ValueError:
        pass
    print("selftest ok")


if __name__ == "__main__":
    if sys.argv[1:] == ["--selftest"]:
        selftest()
        sys.exit(0)
    import yaml

    nodes = yaml.safe_load(open(sys.argv[1]))["nodes"]
    for i, w in enumerate(waves(nodes), 1):
        print(f"wave {i}: {', '.join(w)}")
    for a, b, shared in overlaps(nodes):
        print(f"serial {a} <-> {b}: {', '.join(shared)}")
