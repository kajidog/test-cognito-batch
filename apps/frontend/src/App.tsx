import { BrowserRouter, Routes, Route } from "react-router-dom";
import { ApolloProvider } from "@apollo/client/react";
import { client } from "./graphql/client";
import { CreatePage } from "./pages/CreatePage";
import { ProcessingPage } from "./pages/ProcessingPage";
import { CompletionPage } from "./pages/CompletionPage";

function App() {
  return (
    <ApolloProvider client={client}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<CreatePage />} />
          <Route path="/jobs/:jobId" element={<ProcessingPage />} />
          <Route path="/jobs/:jobId/complete" element={<CompletionPage />} />
        </Routes>
      </BrowserRouter>
    </ApolloProvider>
  );
}

export default App;
