#!/usr/bin/env python3
from pathlib import Path

path = Path("scripts/routingkit_hcbs_query.cpp")
text = path.read_text()

marker = '''enum class Scheduler {
    alternate,
    lower_key,
    smaller_queue,
    lower_key_then_queue,
};

class HCBSQuery {
'''
if text.count(marker) != 1:
    raise SystemExit(f"HCBS class marker: expected one match, got {text.count(marker)}")

queue_code = r'''enum class Scheduler {
    alternate,
    lower_key,
    smaller_queue,
    lower_key_then_queue,
};

// RoutingKit's MinIDQueue::clear() walks every element left in the heap to
// invalidate id_pos. HCBS often terminates with frontier entries still queued,
// so that cleanup is part of every sub-microsecond query. This compatible
// 4-ary queue gives id positions an epoch and makes clear() O(1).
class EpochMinIDQueue {
public:
    EpochMinIDQueue() = default;
    explicit EpochMinIDQueue(unsigned id_count)
        : pos(id_count), pos_epoch(id_count), heap(id_count), current_epoch(1) {}

    bool empty() const { return heap_size == 0; }
    unsigned size() const { return heap_size; }

    bool contains_id(unsigned id) const {
        return pos_epoch[id] == current_epoch;
    }

    void clear() {
        heap_size = 0;
        ++current_epoch;
        if (current_epoch == 0) {
            std::fill(pos_epoch.begin(), pos_epoch.end(), 0);
            current_epoch = 1;
        }
    }

    IDKeyPair peek() const { return heap[0]; }

    IDKeyPair pop() {
        --heap_size;
        const IDKeyPair result = heap[0];
        pos_epoch[result.id] = 0;
        if (heap_size == 0) return result;
        heap[0] = heap[heap_size];
        pos[heap[0].id] = 0;
        move_down(0);
        return result;
    }

    void push(IDKeyPair item) {
        unsigned p = heap_size++;
        heap[p] = item;
        pos[item.id] = p;
        pos_epoch[item.id] = current_epoch;
        move_up(p);
    }

    bool decrease_key(IDKeyPair item) {
        unsigned p = pos[item.id];
        if (heap[p].key <= item.key) return false;
        heap[p].key = item.key;
        move_up(p);
        return true;
    }

private:
    static constexpr unsigned arity = 4;
    std::vector<unsigned> pos;
    std::vector<uint32_t> pos_epoch;
    std::vector<IDKeyPair> heap;
    unsigned heap_size = 0;
    uint32_t current_epoch = 1;

    void swap_positions(unsigned a, unsigned b) {
        std::swap(heap[a], heap[b]);
        pos[heap[a].id] = a;
        pos[heap[b].id] = b;
    }

    void move_up(unsigned p) {
        while (p != 0) {
            const unsigned parent = (p - 1) / arity;
            if (heap[parent].key <= heap[p].key) break;
            swap_positions(parent, p);
            p = parent;
        }
    }

    void move_down(unsigned p) {
        for (;;) {
            const unsigned first = arity * p + 1;
            if (first >= heap_size) return;
            unsigned best = first;
            const unsigned end = std::min(first + arity, heap_size);
            for (unsigned child = first + 1; child < end; ++child) {
                if (heap[child].key < heap[best].key) best = child;
            }
            if (heap[p].key <= heap[best].key) return;
            swap_positions(p, best);
            p = best;
        }
    }
};

template<class Queue>
class HCBSQueryT {
'''
text = text.replace(marker, queue_code, 1)
text = text.replace(
    '    explicit HCBSQuery(const ContractionHierarchy& hierarchy)\n',
    '    explicit HCBSQueryT(const ContractionHierarchy& hierarchy)\n',
    1,
)
text = text.replace(
    '    MinIDQueue forward_queue, backward_queue;\n',
    '    Queue forward_queue, backward_queue;\n',
    1,
)

class_end = '''};

struct Summary {
'''
if text.count(class_end) != 1:
    raise SystemExit(f"class end marker: expected one match, got {text.count(class_end)}")
text = text.replace(
    class_end,
    '''};

using HCBSQuery = HCBSQueryT<MinIDQueue>;
using HCBSEpochQueueQuery = HCBSQueryT<EpochMinIDQueue>;

struct Summary {
''',
    1,
)

old_queries = '''    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
'''
new_queries = '''    ContractionHierarchyQuery standard(ch);
    HCBSQuery hcbs(ch);
    HCBSEpochQueueQuery hcbs_epoch_queue(ch);
'''
if text.count(old_queries) != 1:
    raise SystemExit("query construction marker missing")
text = text.replace(old_queries, new_queries, 1)

old_bench = '''    auto lower_key = benchmark_variant("hcbs-lower-key", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::lower_key);
    });
    auto smaller_queue = benchmark_variant("hcbs-smaller-queue", data, repeats, [&](const Query& q) {
'''
new_bench = '''    auto lower_key = benchmark_variant("hcbs-lower-key", data, repeats, [&](const Query& q) {
        return hcbs.run(q.source, q.target, Scheduler::lower_key);
    });
    auto epoch_queue_lower_key = benchmark_variant("hcbs-epoch-queue-lower-key", data, repeats, [&](const Query& q) {
        return hcbs_epoch_queue.run(q.source, q.target, Scheduler::lower_key);
    });
    auto smaller_queue = benchmark_variant("hcbs-smaller-queue", data, repeats, [&](const Query& q) {
'''
if text.count(old_bench) != 1:
    raise SystemExit("lower-key benchmark marker missing")
text = text.replace(old_bench, new_bench, 1)

old_output = '''    for (const auto& s : {standard_summary, alternate, lower_key, smaller_queue, lower_key_queue}) {
'''
new_output = '''    for (const auto& s : {standard_summary, alternate, lower_key, epoch_queue_lower_key, smaller_queue, lower_key_queue}) {
'''
if text.count(old_output) != 1:
    raise SystemExit("output marker missing")
text = text.replace(old_output, new_output, 1)
path.write_text(text)
