import { createRoot } from "react-dom/client";
import { WorkspaceApp } from "./WorkspaceApp";
import "./styles.css";
import "./workbench.css";
import "./developer.css";
createRoot(document.getElementById("root")!).render(<WorkspaceApp/>);
