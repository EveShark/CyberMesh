"""Debug false positive pipeline state."""
import sys
sys.path.insert(0, r'B:\sentinel')

from sentinel.agents import AnalysisEngine

engine = AnalysisEngine(enable_llm=False, enable_fast_path=True, enable_threat_intel=False)

# Test a clean PE that's causing FP
state = engine.analyze(r'B:\sentinel\data\eval_samples\clean\notepad.exe')

print('=== PIPELINE STATE ===')
print(f'threat_level: {state.get("threat_level")}')
print(f'final_score: {state.get("final_score")}')
print(f'confidence: {state.get("confidence")}')

print('\n=== STATIC RESULTS ===')
for r in state.get('static_results', []):
    print(f'  {r.provider_name}: {r.threat_level.value} (score={r.score:.3f})')

print('\n=== ML RESULTS ===')  
for r in state.get('ml_results', []):
    print(f'  {r.provider_name}: {r.threat_level.value} (score={r.score:.3f})')

print('\n=== FINDINGS ===')
for f in state.get('findings', [])[:15]:
    print(f'  [{f.severity}] {f.source}: {f.description[:60]}')

print('\n=== REASONING STEPS ===')
for step in state.get('reasoning_steps', []):
    print(f'  {step}')

# Calculate vote tallies manually
print('\n=== FINDINGS DETAIL ===')
all_findings = state.get('findings', [])
print(f'Total findings: {len(all_findings)}')
for f in all_findings:
    print(f'  [{f.severity}] {f.source}: {f.description}')

print('\n=== VOTE DEBUG ===')
from sentinel.providers.base import ThreatLevel

WEIGHTS = {
    "entropy_analyzer": 0.15,
    "strings_analyzer": 0.20,
    "malware_pe_ml": 0.30,
}

threat_votes = {ThreatLevel.CLEAN: 0.0, ThreatLevel.SUSPICIOUS: 0.0, ThreatLevel.MALICIOUS: 0.0}
total_weight = 0.0

all_results = state.get('static_results', []) + state.get('ml_results', [])
for r in all_results:
    w = WEIGHTS.get(r.provider_name, 0.1) * r.confidence
    print(f'  {r.provider_name}: {r.threat_level.value} (weight={w:.3f}, conf={r.confidence:.3f})')
    if r.threat_level in threat_votes:
        threat_votes[r.threat_level] += w
        total_weight += w

print(f'\nVote tallies: {dict((k.value, round(v, 3)) for k, v in threat_votes.items())}')
print(f'Total weight: {total_weight:.3f}')

clean_ratio = threat_votes[ThreatLevel.CLEAN] / total_weight if total_weight else 0
sus_ratio = threat_votes[ThreatLevel.SUSPICIOUS] / total_weight if total_weight else 0
print(f'Clean ratio: {clean_ratio:.3f}')
print(f'Suspicious ratio: {sus_ratio:.3f}')
