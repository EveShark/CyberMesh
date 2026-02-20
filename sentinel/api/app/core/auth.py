"""Supabase JWT authentication middleware."""

from typing import Optional, Dict, Any
from fastapi import Request, HTTPException, Depends
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
from jose import jwt, JWTError
from datetime import datetime
import httpx

from ..config import get_settings
from .exceptions import AuthenticationError


class JWTBearer(HTTPBearer):
    """Custom JWT Bearer authentication using Supabase."""
    
    def __init__(self, auto_error: bool = True):
        super().__init__(auto_error=auto_error)
        self._jwks_cache: Optional[Dict] = None
        self._jwks_cache_time: Optional[datetime] = None
    
    async def __call__(self, request: Request) -> Optional[Dict[str, Any]]:
        credentials: HTTPAuthorizationCredentials = await super().__call__(request)
        
        if not credentials:
            if self.auto_error:
                raise AuthenticationError("No authentication credentials provided")
            return None
        
        if credentials.scheme.lower() != "bearer":
            raise AuthenticationError("Invalid authentication scheme. Use Bearer token")
        
        # Verify and decode the token
        payload = await self.verify_token(credentials.credentials)
        
        return payload
    
    async def verify_token(self, token: str) -> Dict[str, Any]:
        """Verify JWT token and return payload."""
        settings = get_settings()
        
        try:
            # Option 1: Use JWT secret (simpler, works for Supabase)
            if settings.supabase_jwt_secret:
                payload = jwt.decode(
                    token,
                    settings.supabase_jwt_secret,
                    algorithms=["HS256"],
                    audience="authenticated",
                )
            else:
                # Option 2: Decode without verification for development
                # In production, always use JWT secret
                payload = jwt.decode(
                    token,
                    options={"verify_signature": False},
                )
            
            # Verify expiration
            exp = payload.get("exp")
            if exp and datetime.utcnow().timestamp() > exp:
                raise AuthenticationError("Token has expired")
            
            return payload
            
        except JWTError as e:
            raise AuthenticationError(f"Invalid token: {str(e)}")


# Dependency for protected routes
jwt_bearer = JWTBearer()


async def get_current_user(
    request: Request,
    payload: Dict[str, Any] = Depends(jwt_bearer)
) -> Dict[str, Any]:
    """Get current authenticated user from JWT payload."""
    
    user_id = payload.get("sub")
    if not user_id:
        raise AuthenticationError("Invalid token: missing user ID")
    
    # Attach user info to request state
    request.state.user_id = user_id
    request.state.user_email = payload.get("email")
    request.state.user_role = payload.get("role", "authenticated")
    
    return {
        "id": user_id,
        "email": payload.get("email"),
        "role": payload.get("role", "authenticated"),
        "aud": payload.get("aud"),
    }


async def get_optional_user(
    request: Request,
) -> Optional[Dict[str, Any]]:
    """Get current user if authenticated, None otherwise."""
    auth_header = request.headers.get("Authorization")
    
    if not auth_header or not auth_header.startswith("Bearer "):
        return None
    
    try:
        token = auth_header.split(" ")[1]
        bearer = JWTBearer(auto_error=False)
        payload = await bearer.verify_token(token)
        
        request.state.user_id = payload.get("sub")
        request.state.user_email = payload.get("email")
        
        return {
            "id": payload.get("sub"),
            "email": payload.get("email"),
            "role": payload.get("role", "authenticated"),
        }
    except:
        return None
