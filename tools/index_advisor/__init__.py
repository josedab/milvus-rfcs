"""
Milvus Index Advisor

Intelligent index recommendation tool for Milvus vector database.
"""

from .advisor import IndexAdvisor, IndexType, UseCase, Recommendation

__version__ = "1.0.0"
__all__ = ["IndexAdvisor", "IndexType", "UseCase", "Recommendation"]
