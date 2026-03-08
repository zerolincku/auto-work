import i18n from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";

import enUS from "./locales/en-US";
import zhCN from "./locales/zh-CN";

export const LANGUAGE_STORAGE_KEY = "auto-work-language";

const resources = {
  "zh-CN": zhCN,
  "en-US": enUS,
};

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: ["en-US", "zh-CN"],
    supportedLngs: ["zh-CN", "en-US"],
    nonExplicitSupportedLngs: false,
    defaultNS: "translation",
    ns: ["translation"],
    returnNull: false,
    returnEmptyString: false,
    interpolation: {
      escapeValue: false,
    },
    detection: {
      order: ["localStorage", "navigator"],
      lookupLocalStorage: LANGUAGE_STORAGE_KEY,
      caches: ["localStorage"],
    },
    parseMissingKeyHandler: (key) => {
      const tail = key.split(".").pop() || key;
      return tail
        .replace(/([A-Z])/g, " $1")
        .replace(/_/g, " ")
        .trim();
    },
  });

export default i18n;
