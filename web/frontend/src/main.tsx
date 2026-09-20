import { setNonce } from "get-nonce";
import { createRoot } from "react-dom/client";
import { App } from "./app";
import "./style.css";

// Shared Radix modal styles use the server's per-response CSP nonce.
const nonce = document.querySelector<HTMLMetaElement>(
  'meta[name="candela-style-nonce"]',
)?.content;
if (nonce) setNonce(nonce);

createRoot(document.getElementById("app")!).render(<App />);
