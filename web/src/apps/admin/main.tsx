import React from "react";
import ReactDOM from "react-dom/client";
import { BrandProvider } from "../../lib/brand";
import { initTheme } from "../../lib/theme";
import { ToastHost } from "../../components/ui";
import AdminApp from "./AdminApp";
import "../../styles/theme.css";

initTheme();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrandProvider>
      <AdminApp />
      <ToastHost />
    </BrandProvider>
  </React.StrictMode>,
);
