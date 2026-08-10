#!/usr/bin/env python3
from pathlib import Path

path = Path("scripts/hierarchy_interleaved_benchmark.cpp")
text = path.read_text()
old = '''static uint64_t percentile(std::vector<uint64_t> values, double p) {
    if (values.empty()) return 0;
    std::sort(values.begin(), values.end());
    const size_t index = static_cast<size_t>(p * static_cast<double>(values.size() - 1));
    return values[index];
}
'''
new = old.replace('percentile(', 'hierarchy_percentile(', 1)
if text.count(old) != 1:
    raise SystemExit(f"expected one local percentile definition, got {text.count(old)}")
text = text.replace(old, new, 1)
text = text.replace('percentile(query_medians[algorithm], .50)', 'hierarchy_percentile(query_medians[algorithm], .50)')
text = text.replace('percentile(query_medians[algorithm], .95)', 'hierarchy_percentile(query_medians[algorithm], .95)')
text = text.replace('percentile(query_medians[algorithm], .99)', 'hierarchy_percentile(query_medians[algorithm], .99)')
path.write_text(text)
