# Retrieval MCP deployments

Obot remains a governance and MCP gateway. Retrieval runs as a separate, read-only MCP service.

Enable the chart's `rag` section with an image that implements Streamable HTTP MCP at `/mcp`, exposes `search_documents` and `get_document`, and validates a caller's LDAP groups for every returned document. The chart creates separate `engineering`, `finance`, and `general` deployments and passes the collection plus allowed LDAP groups as `RAG_COLLECTION` and `RAG_ALLOWED_GROUPS`.

Register each resulting service in Obot as a remote MCP server, for example `http://<release>-rag-engineering:<port>/mcp`, then grant access with the existing MCP access policies using `ldap/engineering`, `ldap/finance`, and `ldap/general`. The MCP server must still enforce document-level access itself; Obot's policy only controls access to the endpoint.
