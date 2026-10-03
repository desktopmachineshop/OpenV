// The one axios instance every API call goes through, its two interceptors
// (the active workspace header on the way out; the sign-in and
// verify-email redirects on the way back), the upload config and the
// download helpers. Loading this module creates the instance and installs
// the interceptors, in that order; api/client loads it before any area
// module. Nothing outside src/api imports it (refactor plan F1, K12).

import axios, { AxiosInstance, AxiosRequestConfig } from 'axios';
import { filenameFromContentDisposition } from './contentDisposition';
import { isPublicPath } from '../utils/publicPaths';
import { getAPIBaseURL } from './baseURL';
import { ORG_HEADER, readActiveOrg } from '../state/activeOrgStorage';
import type { UploadProgressHandler } from './types/attachments';

const API_BASE_URL = getAPIBaseURL();

const client: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: 60000, // 60 second timeout for complex operations like template import
  withCredentials: true, // session cookie auth
});

// Attach the active workspace (org) to every request. The backend validates
// membership and falls back server-side when the header is invalid.
// This tab's workspace wins over the browser's last used; with neither (or
// no storage) the request goes without the header.
client.interceptors.request.use((config) => {
  const { tabOrg, lastUsed } = readActiveOrg();
  const activeOrg = tabOrg || lastUsed;
  if (activeOrg) {
    config.headers[ORG_HEADER] = activeOrg;
  }
  return config;
});

/**
 * The config every multipart upload in this client shares.
 *
 * `timeout: 0` is the point of it. The instance-wide 60 s above is sized for
 * JSON, and axios counts its timeout as wall-clock across the WHOLE request —
 * the request body included — so on an upload it was never a server deadline
 * but a cap on how long the member's connection had to push the file. That
 * made the real ceiling their upstream bandwidth rather than any limit OpenV
 * publishes: a file well inside the workspace's plan died at
 * "timeout of 60000ms exceeded" without the API ever seeing the request.
 * What may be uploaded is a SIZE, and the API is what decides it — see
 * uploadLimitBytes, which answers with the workspace's own number.
 */
const uploadConfig = (onProgress?: UploadProgressHandler): AxiosRequestConfig => ({
  headers: { 'Content-Type': 'multipart/form-data' },
  timeout: 0,
  onUploadProgress: onProgress
    ? (event) => {
        // event.total is absent when the body's length is unknown; report
        // null rather than a percentage computed against nothing.
        if (!event.total) {
          onProgress(null);
          return;
        }
        onProgress(Math.min(100, Math.round((event.loaded * 100) / event.total)));
      }
    : undefined,
});

// Add response interceptor for better error handling + auth redirects
client.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', {
      url: error.config?.url,
      method: error.config?.method,
      status: error.response?.status,
      data: error.response?.data,
      message: error.message,
    });
    // Redirect to login on 401, except on public pages and auth calls.
    if (
      error.response?.status === 401 &&
      typeof window !== 'undefined' &&
      !isPublicPath(window.location.pathname) &&
      !String(error.config?.url || '').includes('/api/v1/auth/')
    ) {
      window.location.href = '/login';
    }
    // An unverified account meets the wall: the server refuses everything
    // but the auth endpoints until the emailed link is clicked.
    if (
      error.response?.status === 403 &&
      error.response?.data?.code === 'email_unverified' &&
      typeof window !== 'undefined' &&
      !window.location.pathname.startsWith('/verify-email')
    ) {
      window.location.href = '/verify-email';
    }
    return Promise.reject(error);
  }
);

// Hand the browser a file to save. One copy of the anchor dance, used by every
// download: the object URL is revoked straight after the click so a large
// export is not held in memory for the life of the tab.
const saveBlob = (data: BlobPart, filename: string): void => {
  const url = window.URL.createObjectURL(new Blob([data]));
  const link = document.createElement('a');
  link.href = url;
  link.setAttribute('download', filename);
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.URL.revokeObjectURL(url);
};

const downloadBlob = async (url: string, fallbackName: string) => {
  const response = await client.get(url, { responseType: 'blob' });
  const filename =
    filenameFromContentDisposition(response.headers['content-disposition']) || fallbackName;
  const objectUrl = window.URL.createObjectURL(new Blob([response.data]));
  const anchor = document.createElement('a');
  anchor.href = objectUrl;
  anchor.setAttribute('download', filename);
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.URL.revokeObjectURL(objectUrl);
  return response;
};

export { API_BASE_URL, client, uploadConfig, saveBlob, downloadBlob };
