self.__BUILD_MANIFEST = {
  "__rewrites": {
    "afterFiles": [
      {
        "source": "/api/:path*"
      },
      {
        "source": "/ws"
      },
      {
        "source": "/auth/:path*"
      },
      {
        "source": "/uploads/:path*"
      }
    ],
    "beforeFiles": [
      {
        "source": "/docs"
      },
      {
        "source": "/docs/:path*"
      }
    ],
    "fallback": []
  },
  "sortedPages": [
    "/_app",
    "/_error"
  ]
};self.__BUILD_MANIFEST_CB && self.__BUILD_MANIFEST_CB()