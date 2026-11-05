import { createRoot } from "react-dom/client";
import App from "./App";
import "./styles.css";

// No StrictMode on purpose: the enrollment-code dialog issues a code when it
// opens, and development double-mounting would issue two.
createRoot(document.getElementById("root")!).render(<App />);
