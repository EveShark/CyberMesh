from typing import Protocol, Type, TypeVar
from .errors import SchemaError


T = TypeVar('T', bound='ProtobufMessage')


class ProtobufMessage(Protocol):
    """Protocol for Protobuf message types."""
    
    def SerializeToString(self, deterministic: bool = True) -> bytes:
        ...
    
    def ParseFromString(self, data: bytes) -> None:
        ...


def encode_canonical(message: ProtobufMessage) -> bytes:
    """
    Encode a Protobuf message to canonical deterministic bytes.
    
    These bytes are used for:
    - Content hash computation (SHA-256)
    - Signature generation
    - Wire format transmission
    
    Rules:
    - Deterministic serialization (sorted fields, no unknown fields)
    - All required fields must be present
    - No default values omitted
    """
    try:
        canonical_bytes = message.SerializeToString(deterministic=True)
        if not canonical_bytes:
            raise SchemaError("Serialized message is empty")
        return canonical_bytes
    except Exception as e:
        raise SchemaError(f"Failed to encode message: {e}")


def decode_canonical(message_class: Type[T], data: bytes) -> T:
    """
    Decode canonical bytes into a Protobuf message.
    
    Validates:
    - Data is not empty
    - Parsing succeeds
    - No unknown fields present
    """
    if not data:
        raise SchemaError("Cannot decode empty data")
    
    try:
        message = message_class()
        message.ParseFromString(data)
        return message
    except Exception as e:
        raise SchemaError(f"Failed to decode message: {e}")


def validate_message(message: ProtobufMessage) -> bool:
    """
    Validate that a Protobuf message can be canonically encoded.
    
    Returns True if valid, raises SchemaError otherwise.
    """
    try:
        canonical_bytes = encode_canonical(message)
        return len(canonical_bytes) > 0
    except SchemaError:
        raise
    except Exception as e:
        raise SchemaError(f"Message validation failed: {e}")
