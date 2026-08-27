import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { registerWebMCPTools } from './webmcp/register'

// Registered synchronously, at module evaluation — before React ever mounts
// — so navigator.modelContext.getTools() already sees all 7 tools the
// instant an agent inspects the document, with no network round trip or
// render in between. See web/src/webmcp/register.ts and
// web/src/canvasBootstrap.ts for how each tool resolves its canvas id lazily
// once it actually runs.
registerWebMCPTools()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
