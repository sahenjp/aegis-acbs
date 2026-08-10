#include <routingkit/customizable_contraction_hierarchy.h>
#include <routingkit/nested_dissection.h>
#include <routingkit/constants.h>

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

using namespace RoutingKit;
using Clock = std::chrono::steady_clock;

struct Query { unsigned source, target, expected_distance; bool expected_reachable; };

static long long ns(Clock::duration d){ auto x=std::chrono::duration_cast<std::chrono::nanoseconds>(d).count(); return x<1?1:x; }
static long long pct(std::vector<long long> v,double p){ if(v.empty())return 0; std::sort(v.begin(),v.end()); size_t i=(size_t)(p*v.size()); if(i>=v.size())i=v.size()-1; return v[i]; }

int main(int argc,char**argv){
    if(argc<3||argc>4){ std::cerr<<"usage: routingkit_cch_baseline INPUT OUTPUT [REPEATS]\n"; return 2; }
    int repeats=argc==4?std::stoi(argv[3]):3;
    std::ifstream in(argv[1]); if(!in) throw std::runtime_error("cannot open input");
    std::string magic; in>>magic; if(magic!="AEGIS_ROUTINGKIT_CCH_V1") throw std::runtime_error("bad input magic");
    unsigned n; size_t m,qc; in>>n>>m>>qc;
    std::vector<float> lat(n),lon(n); for(unsigned i=0;i<n;++i) in>>lat[i]>>lon[i];
    std::vector<unsigned> tail(m),head(m),weight(m); for(size_t i=0;i<m;++i){ unsigned long long w; in>>tail[i]>>head[i]>>w; if(w>=inf_weight) throw std::runtime_error("weight out of range"); weight[i]=(unsigned)w; }
    std::vector<Query> qs(qc); for(size_t i=0;i<qc;++i){ unsigned long long d; int r; in>>qs[i].source>>qs[i].target>>d>>r; qs[i].expected_distance=(unsigned)d; qs[i].expected_reachable=r!=0; }

    auto order_begin=Clock::now();
    auto order=compute_nested_node_dissection_order_using_inertial_flow(n,tail,head,lat,lon);
    long long order_ns=ns(Clock::now()-order_begin);
    auto topo_begin=Clock::now();
    CustomizableContractionHierarchy cch(order,tail,head);
    long long topology_ns=ns(Clock::now()-topo_begin);
    auto customize_begin=Clock::now();
    CustomizableContractionHierarchyMetric metric(cch,weight); metric.customize();
    long long customize_ns=ns(Clock::now()-customize_begin);
    CustomizableContractionHierarchyQuery query(metric);
    for(size_t i=0;i<std::min<size_t>(3,qs.size());++i){ query.reset().add_source(qs[i].source).add_target(qs[i].target).run(); (void)query.get_distance(); }
    std::vector<long long> durations; durations.reserve(qc); bool ok=true;
    for(const auto& q:qs){ std::vector<long long> runs; unsigned d=inf_weight; for(int r=0;r<repeats;++r){ auto b=Clock::now(); query.reset().add_source(q.source).add_target(q.target).run(); d=query.get_distance(); runs.push_back(ns(Clock::now()-b)); } std::sort(runs.begin(),runs.end()); durations.push_back(runs[runs.size()/2]); bool reachable=d!=inf_weight; ok &= reachable==q.expected_reachable && (!reachable||d==q.expected_distance); }
    long long total=0; for(auto d:durations) total+=d;
    std::ofstream out(argv[2]); if(!out) throw std::runtime_error("cannot open output");
    out<<"{\n  \"nodes\": "<<n<<",\n  \"edges\": "<<m<<",\n  \"queries\": "<<qc<<",\n  \"orderNs\": "<<order_ns<<",\n  \"topologyNs\": "<<topology_ns<<",\n  \"customizeNs\": "<<customize_ns<<",\n  \"meanNs\": "<<(durations.empty()?0:total/(long long)durations.size())<<",\n  \"medianNs\": "<<pct(durations,.50)<<",\n  \"p95Ns\": "<<pct(durations,.95)<<",\n  \"p99Ns\": "<<pct(durations,.99)<<",\n  \"allCorrect\": "<<(ok?"true":"false")<<"\n}\n";
    std::cout<<"RoutingKit CCH: order="<<order_ns/1e9<<"s topology="<<topology_ns/1e9<<"s customize="<<customize_ns/1e9<<"s median="<<pct(durations,.50)/1e6<<"ms p95="<<pct(durations,.95)/1e6<<"ms p99="<<pct(durations,.99)/1e6<<"ms correct="<<(ok?"true":"false")<<"\n";
    return ok?0:3;
}
