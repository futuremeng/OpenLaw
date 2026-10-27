#!/usr/bin/env python3
"""合并 OpenCourt 清单分片（按 URL 去重）+ 区段覆盖报告。
用法: python3 govmerge.py <分片目录> <输出 merged.json>"""
import json, sys, glob, os, re, collections

d, out = sys.argv[1], sys.argv[2]
files = sorted(glob.glob(os.path.join(d, 'list_agent_*.json')))

def norm(u):
    # 主站 http/https 两形态并一条（与 OpenLaw LoadGovcnList 归一策略一致）；
    # 地方政府直链不归一（如实保留原 URL）。
    return 'https://' + u[len('http://'):] if u.startswith('http://www.gov.cn/') else u

seen, merged = {}, []
for f in files:
    with open(f) as fh:
        entries = json.load(fh)
    n0 = len(merged)
    for e in entries:
        u = e.get('url', '')
        k = norm(u)
        if u and k not in seen:
            seen[k] = f
            merged.append(e)
    print(f'{os.path.basename(f)}: {len(entries)} 条（新增 {len(merged)-n0}）')

cats = collections.Counter(e.get('cat') or '(空)' for e in merged)
hosts = collections.Counter(e['url'].split('/')[2] for e in merged)
print(f'\n合计 {len(merged)} 条（分片 {len(files)} 份，按 URL 去重）')
print('分类:', dict(cats))
top = hosts.most_common(8)
print('主机:', dict(top), '…' if len(hosts) > 8 else '')
print(f'非 www.gov.cn 条目: {sum(v for h, v in hosts.items() if h != "www.gov.cn")}')

with open(out, 'w') as fh:
    json.dump(merged, fh, ensure_ascii=False, indent=2)
print(f'→ {out}')

# 区段覆盖（来自采集日志会话记录，供补采参考）
try:
    log = open('/tmp/govcollect.log').read()
    for m in re.finditer(r'浏览 (\d+) 页（起始页 (\d+)）得 (\d+) 条（(\w+)）', log):
        pages, start, n, stop = int(m.group(1)), int(m.group(2)), int(m.group(3)), m.group(4)
        print(f'区段 {start}-{start+pages-1}: {n} 条（{stop}）')
except FileNotFoundError:
    pass
