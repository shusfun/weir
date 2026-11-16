"""Expose the standard collections aliases used by the pinned SSR server."""
import collections
import collections.abc

for alias in ('MutableMapping', 'Mapping', 'Iterable', 'MutableSet'):
    setattr(collections, alias, getattr(collections.abc, alias))
