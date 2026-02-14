import React, { useState } from 'react';
import { useAppStore } from './state/store';
import { ModuleView } from './views/ModuleView';
import { ProjectList } from './components/ProjectList';
import './index.css';

function App() {
  const { projectId, setProjectId } = useAppStore();
  const [showProjectList, setShowProjectList] = useState(!projectId);

  const handleSwitchProject = () => {
    setProjectId('');
    setShowProjectList(true);
  };

  React.useEffect(() => {
    if (projectId && showProjectList) {
      setShowProjectList(false);
    }
  }, [projectId, showProjectList]);

  return (
    <div className="app-container">
      {showProjectList ? (
        <ProjectList />
      ) : (
        <>
          {projectId && (
            <ModuleView onSwitchProject={handleSwitchProject} />
          )}
        </>
      )}
    </div>
  );
}

export default App;
