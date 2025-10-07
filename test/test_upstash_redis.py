"""Quick test of Upstash Redis connection"""
from dotenv import load_dotenv
import redis
import os

load_dotenv('ai-service/.env')

protocol = 'rediss' if os.getenv('REDIS_TLS_ENABLED') == 'true' else 'redis'
host = os.getenv('REDIS_HOST')
port = os.getenv('REDIS_PORT')
password = os.getenv('REDIS_PASSWORD')

url = f"{protocol}://default:{password}@{host}:{port}/0"
print(f"Connecting to: {protocol}://{host}:{port}/0")
print(f"TLS: {os.getenv('REDIS_TLS_ENABLED')}")

try:
    client = redis.from_url(url, decode_responses=True, ssl_cert_reqs=None)
    result = client.ping()
    print(f"PING: {result}")
    
    client.set('test_key', 'test_value')
    value = client.get('test_key')
    print(f"SET/GET: {value}")
    
    print("\nSUCCESS - Redis connection working!")
except Exception as e:
    print(f"ERROR: {e}")
    import traceback
    traceback.print_exc()
