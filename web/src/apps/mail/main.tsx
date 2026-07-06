import React from "react";
import ReactDOM from "react-dom/client";
import { BrandProvider } from "../../lib/brand";
import { initTheme } from "../../lib/theme";
import { ToastHost } from "../../components/ui";
import MailApp from "./MailApp";
import "../../styles/theme.css";

initTheme();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrandProvider>
      <MailApp />
      <ToastHost />
    </BrandProvider>
  </React.StrictMode>,
);
