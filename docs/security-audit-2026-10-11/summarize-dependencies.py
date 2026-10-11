"""汇总 govulncheck 的 JSON 对象流，按公告去重并保留模块及调用路径证据。"""
import json
from collections import defaultdict
from pathlib import Path

root = Path(__file__).resolve().parent
raw = (root / "govulncheck-local-go.json").read_text(encoding="utf-8-sig")
decoder = json.JSONDecoder()
records = []
offset = 0
while offset < len(raw):
    while offset < len(raw) and raw[offset].isspace():
        offset += 1
    if offset == len(raw):
        break
    record, offset = decoder.raw_decode(raw, offset)
    records.append(record)

advisories = {r["osv"]["id"]: r["osv"] for r in records if "osv" in r}
findings = [r["finding"] for r in records if "finding" in r]
symbol_findings = [f for f in findings if any(t.get("function") for t in f.get("trace", []))]
groups = defaultdict(list)
for finding in symbol_findings:
    groups[finding["osv"]].append(finding)

rows = []
module_ids = defaultdict(set)
for advisory_id, matches in sorted(groups.items()):
    affected = set()
    for match in matches:
        leaf = match["trace"][0]
        affected.add((leaf.get("module"), leaf.get("version"), match.get("fixed_version")))
        module_ids[leaf.get("module")].add(advisory_id)
    rows.append({
        "id": advisory_id,
        "summary": advisories[advisory_id].get("summary"),
        "url": "https://pkg.go.dev/vuln/" + advisory_id,
        "affected": [{"module": m, "version": v, "fixed": f} for m, v, f in sorted(affected)],
        "example_trace": matches[0]["trace"],
    })

result = {
    "config": next(r["config"] for r in records if "config" in r),
    "note": "静态调用路径可达提示；不等同于已验证的运行时可利用漏洞。模块间公告可能重叠。",
    "unique_symbol_advisories": len(groups),
    "advisories_per_module": {m: len(ids) for m, ids in sorted(module_ids.items())},
    "advisories": rows,
}
(root / "dependency-summary.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps({k: v for k, v in result.items() if k != "advisories"}, ensure_ascii=False, indent=2))
