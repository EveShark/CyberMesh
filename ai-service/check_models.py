import joblib
import os

print("=" * 60)
print("MODEL COMPARISON REPORT")
print("=" * 60)

# Load current ddos model
ddos_new = joblib.load('data/models/ddos.pkl')
print("\n=== CURRENT MODEL (ddos.pkl) ===")
print(f"Type: {type(ddos_new).__name__}")
print(f"Features: {ddos_new.n_features_in_}")
print(f"Size: {os.path.getsize('data/models/ddos.pkl') / (1024*1024):.1f} MB")
print(f"Estimators: {ddos_new.n_estimators}")

# Load old ddos model
ddos_old = joblib.load('data/models/old models/ddos_v2.pkl')
print("\n=== OLD MODEL (ddos_v2.pkl) ===")
print(f"Type: {type(ddos_old).__name__}")
print(f"Features: {ddos_old.n_features_in_}")
print(f"Size: {os.path.getsize('data/models/old models/ddos_v2.pkl') / (1024*1024):.1f} MB")
print(f"Estimators: {ddos_old.n_estimators}")

# Load anomaly model
ano_model = joblib.load('data/models/anomaly.pkl')
print("\n=== ANOMALY MODEL (anomaly.pkl) ===")
print(f"Type: {type(ano_model).__name__}")
print(f"Features: {ano_model.n_features_in_}")
print(f"Size: {os.path.getsize('data/models/anomaly.pkl') / (1024*1024):.1f} MB")

# Comparison
print("\n" + "=" * 60)
print("EVOLUTION SUMMARY")
print("=" * 60)
print(f"DDoS v2 -> v3: {ddos_old.n_features_in_} -> {ddos_new.n_features_in_} features (+{ddos_new.n_features_in_ - ddos_old.n_features_in_})")
print(f"Size increase: {(os.path.getsize('data/models/ddos.pkl') / os.path.getsize('data/models/old models/ddos_v2.pkl') - 1) * 100:.1f}%")
print(f"Anomaly model: {ano_model.n_features_in_} features (legacy)")
print("\nREADY FOR: Schema-driven extraction with multiple feature counts")
