#!/usr/bin/env python3
from pathlib import Path

path = Path("scripts/routingkit_hcbs_query.cpp")
text = path.read_text()

marker = '''    unsigned run(unsigned external_source, unsigned external_target, Scheduler scheduler) {
        begin_query();
'''
if text.count(marker) != 1:
    raise SystemExit(f"run marker: expected one match, got {text.count(marker)}")

hot = '''    unsigned run_lower_key_hot(unsigned external_source, unsigned external_target) {
        begin_query();
        const unsigned source = ch.rank[external_source];
        const unsigned target = ch.rank[external_target];
        mark_forward(source, 0);
        mark_backward(target, 0);
        unsigned incumbent = inf_weight;

        for (;;) {
            const bool forward_finished = forward_queue.empty() || forward_queue.peek().key >= incumbent;
            const bool backward_finished = backward_queue.empty() || backward_queue.peek().key >= incumbent;
            if (forward_finished && backward_finished) break;

            if (forward_finished) {
                settle_backward(incumbent);
            } else if (backward_finished) {
                settle_forward(incumbent);
            } else if (forward_queue.peek().key <= backward_queue.peek().key) {
                settle_forward(incumbent);
            } else {
                settle_backward(incumbent);
            }
        }
        return incumbent;
    }

'''
text = text.replace(marker, hot + marker, 1)

old = '''    auto lower_key = benchmark_variant("hcbs-lower-key", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::lower_key);
    });
    auto smaller_queue = benchmark_variant("hcbs-smaller-queue", data, repeats, [&](const Query& q) {
'''
new = '''    auto lower_key = benchmark_variant("hcbs-lower-key", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::lower_key);
    });
    auto lower_key_hot = benchmark_variant("hcbs-lower-key-hot", data, repeats, [&](const Query& q) {
        return hcbs.run_lower_key_hot(q.source, q.target);
    });
    auto smaller_queue = benchmark_variant("hcbs-smaller-queue", data, repeats, [&](const Query& q) {
'''
if text.count(old) != 1:
    raise SystemExit(f"benchmark marker: expected one match, got {text.count(old)}")
text = text.replace(old, new, 1)

old = '''    for (const auto& s : {standard_summary, alternate, lower_key, smaller_queue, lower_key_queue}) {
'''
new = '''    for (const auto& s : {standard_summary, alternate, lower_key, lower_key_hot, smaller_queue, lower_key_queue}) {
'''
if text.count(old) != 1:
    raise SystemExit(f"output marker: expected one match, got {text.count(old)}")
text = text.replace(old, new, 1)
path.write_text(text)
