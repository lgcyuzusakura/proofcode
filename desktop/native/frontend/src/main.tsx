import { createRoot } from "react-dom/client";
import { WorkspaceApp } from "../../../../frontend/src/WorkspaceApp";
import "../../../../frontend/src/styles.css";
import "../../../../frontend/src/workbench.css";
createRoot(document.getElementById("root")!).render(<WorkspaceApp/>);
