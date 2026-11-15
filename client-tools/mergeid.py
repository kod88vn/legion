"""mergeid.py <grf.tsv> <overlay text lub> <out> <tablename>
Union of an ID table: GRF (authoritative) + names only present in the overlay text file."""
import re, sys
tsv, ovl, out, tname = sys.argv[1:5]
grf = {}
for line in open(tsv, encoding='utf-8'):
    t, k, v = line.rstrip('\n').split('\t')
    if t == tname: grf[k] = v
txt = open(ovl, encoding='latin-1').read()
ov = dict(re.findall(r'^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(-?\d+)\s*,?', txt, re.M))
extra = {k: v for k, v in ov.items() if k not in grf}
conf = [k for k, v in ov.items() if k in grf and grf[k] != v]
with open(out, 'w', encoding='latin-1', newline='\r\n') as f:
    f.write('-- AI-Lab merged %s: kRO GRF (%d) + ROenglishRE-only names (%d)\n%s = {\n' % (tname, len(grf), len(extra), tname))
    for k, v in list(grf.items()) + list(extra.items()):
        f.write('\t%s = %s,\n' % (k, v))
    f.write('}\n')
print('grf', len(grf), 'overlay', len(ov), 'extra', len(extra), 'conflicts', len(conf), conf[:10])
