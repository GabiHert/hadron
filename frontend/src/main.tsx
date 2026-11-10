import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { installErrorLogging } from "./errorlog";
import { applyCachedTheme } from "./theme";
import { ErrorBoundary } from "./components/ErrorBoundary";
import "./styles/fonts.css";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/app.css";

installErrorLogging(window);
applyCachedTheme();

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </React.StrictMode>,
);
