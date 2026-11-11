from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ReadRequest(_message.Message):
    __slots__ = ("fact_ids",)
    FACT_IDS_FIELD_NUMBER: _ClassVar[int]
    fact_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, fact_ids: _Optional[_Iterable[str]] = ...) -> None: ...

class Fact(_message.Message):
    __slots__ = ("fact_id", "type", "data")
    FACT_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    fact_id: str
    type: str
    data: bytes
    def __init__(self, fact_id: _Optional[str] = ..., type: _Optional[str] = ..., data: _Optional[bytes] = ...) -> None: ...

class ReadResponse(_message.Message):
    __slots__ = ("facts",)
    FACTS_FIELD_NUMBER: _ClassVar[int]
    facts: _containers.RepeatedCompositeFieldContainer[Fact]
    def __init__(self, facts: _Optional[_Iterable[_Union[Fact, _Mapping]]] = ...) -> None: ...

class ReturnLimitRequest(_message.Message):
    __slots__ = ("origin_movement_id",)
    ORIGIN_MOVEMENT_ID_FIELD_NUMBER: _ClassVar[int]
    origin_movement_id: str
    def __init__(self, origin_movement_id: _Optional[str] = ...) -> None: ...

class ReturnLimitResponse(_message.Message):
    __slots__ = ("known", "limited", "quantity")
    KNOWN_FIELD_NUMBER: _ClassVar[int]
    LIMITED_FIELD_NUMBER: _ClassVar[int]
    QUANTITY_FIELD_NUMBER: _ClassVar[int]
    known: bool
    limited: bool
    quantity: str
    def __init__(self, known: _Optional[bool] = ..., limited: _Optional[bool] = ..., quantity: _Optional[str] = ...) -> None: ...
