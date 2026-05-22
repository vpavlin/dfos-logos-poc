cmake_minimum_required(VERSION 3.14)
project(DfosModulePlugin LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)

include(GNUInstallDirs)

# ── Dependency roots ──────────────────────────────────────────────────────────

# logos-cpp-sdk: either source checkout or installed package
if(NOT DEFINED LOGOS_CPP_SDK_ROOT)
    if(DEFINED ENV{LOGOS_CPP_SDK_ROOT})
        set(LOGOS_CPP_SDK_ROOT "$ENV{LOGOS_CPP_SDK_ROOT}")
    else()
        set(LOGOS_CPP_SDK_ROOT "${CMAKE_CURRENT_SOURCE_DIR}/vendor/logos-cpp-sdk")
    endif()
endif()

# logos-liblogos: provides PluginInterface / interface.h
if(NOT DEFINED LOGOS_LIBLOGOS_ROOT)
    if(DEFINED ENV{LOGOS_LIBLOGOS_ROOT})
        set(LOGOS_LIBLOGOS_ROOT "$ENV{LOGOS_LIBLOGOS_ROOT}")
    else()
        set(LOGOS_LIBLOGOS_ROOT "${CMAKE_CURRENT_SOURCE_DIR}/vendor/logos-liblogos")
    endif()
endif()

# ── Detect layout ─────────────────────────────────────────────────────────────

if(EXISTS "${LOGOS_CPP_SDK_ROOT}/cpp/logos_api.h")
    set(_cpp_sdk_is_source TRUE)
elseif(EXISTS "${LOGOS_CPP_SDK_ROOT}/include/cpp/logos_api.h")
    set(_cpp_sdk_is_source FALSE)
else()
    message(FATAL_ERROR "logos-cpp-sdk not found at ${LOGOS_CPP_SDK_ROOT}")
endif()

if(EXISTS "${LOGOS_LIBLOGOS_ROOT}/src/common/interface.h")
    set(_interface_h "${LOGOS_LIBLOGOS_ROOT}/src/common/interface.h")
    set(_interface_dir "${LOGOS_LIBLOGOS_ROOT}/src/common")
elseif(EXISTS "${LOGOS_LIBLOGOS_ROOT}/interface.h")
    set(_interface_h "${LOGOS_LIBLOGOS_ROOT}/interface.h")
    set(_interface_dir "${LOGOS_LIBLOGOS_ROOT}")
elseif(EXISTS "${LOGOS_LIBLOGOS_ROOT}/include/interface.h")
    set(_interface_h "${LOGOS_LIBLOGOS_ROOT}/include/interface.h")
    set(_interface_dir "${LOGOS_LIBLOGOS_ROOT}/include")
else()
    message(FATAL_ERROR "interface.h not found under LOGOS_LIBLOGOS_ROOT=${LOGOS_LIBLOGOS_ROOT}")
endif()

get_filename_component(LOGOS_DEPS_ROOT "${LOGOS_CPP_SDK_ROOT}" DIRECTORY)

# ── Qt ────────────────────────────────────────────────────────────────────────

find_package(QT NAMES Qt6 Qt5 REQUIRED COMPONENTS Core RemoteObjects)
if(Qt6_FOUND)
    set(QT_VERSION_MAJOR 6)
else()
    set(QT_VERSION_MAJOR 5)
endif()
find_package(Qt${QT_VERSION_MAJOR} REQUIRED COMPONENTS Core RemoteObjects)

# ── logos-cpp-generator ───────────────────────────────────────────────────────

set(METADATA_JSON "${CMAKE_CURRENT_SOURCE_DIR}/metadata.json")
set(GENERATED_DIR "${CMAKE_BINARY_DIR}/generated_code")
file(MAKE_DIRECTORY "${GENERATED_DIR}")

# Prefer the pre-built generator from the workspace build.
if(NOT DEFINED CPP_GENERATOR)
    find_program(CPP_GENERATOR logos-cpp-generator
        PATHS "${LOGOS_DEPS_ROOT}/build/cpp-generator/bin"
              "${LOGOS_CPP_SDK_ROOT}/bin"
        NO_DEFAULT_PATH)
endif()

if(CPP_GENERATOR)
    add_custom_command(
        OUTPUT  "${GENERATED_DIR}/logos_sdk.cpp" "${GENERATED_DIR}/logos_sdk.h"
        COMMAND "${CPP_GENERATOR}"
                --metadata "${METADATA_JSON}"
                --general-only
                --output-dir "${GENERATED_DIR}"
        DEPENDS "${METADATA_JSON}"
        COMMENT "Generating logos_sdk.cpp for dfos_module"
        VERBATIM
    )
else()
    message(FATAL_ERROR "logos-cpp-generator not found. Set CPP_GENERATOR or build it first.")
endif()

# ── dfos.so ───────────────────────────────────────────────────────────────────

set(DFOS_LIB_DIR "${CMAKE_CURRENT_SOURCE_DIR}/lib")

# find_library strips .so/.dylib extensions when searching, so we locate it
# manually to handle the unconventional dfos.so name (no lib prefix).
set(DFOS_LIB_PATH "")
foreach(_name dfos.so libdfos.so)
    if(EXISTS "${DFOS_LIB_DIR}/${_name}")
        set(DFOS_LIB_PATH "${DFOS_LIB_DIR}/${_name}")
        break()
    endif()
endforeach()
if(NOT DFOS_LIB_PATH)
    message(FATAL_ERROR "dfos.so not found in ${DFOS_LIB_DIR}. Build dfos-capi first.")
endif()
message(STATUS "Found dfos shared library: ${DFOS_LIB_PATH}")

# ── Plugin target ─────────────────────────────────────────────────────────────

set(PLUGIN_SOURCES
    src/dfos_module_plugin.h
    src/dfos_module_plugin.cpp
    "${_interface_h}"
    "${GENERATED_DIR}/logos_sdk.cpp"
)

if(_cpp_sdk_is_source)
    # Core SDK sources
    foreach(_src
            logos_api.cpp logos_api.h
            logos_api_client.cpp logos_api_client.h
            logos_api_consumer.cpp logos_api_consumer.h
            logos_api_provider.cpp logos_api_provider.h
            token_manager.cpp token_manager.h
            module_proxy.cpp module_proxy.h)
        if(EXISTS "${LOGOS_CPP_SDK_ROOT}/cpp/${_src}")
            list(APPEND PLUGIN_SOURCES "${LOGOS_CPP_SDK_ROOT}/cpp/${_src}")
        endif()
    endforeach()
    # Optional newer SDK sources
    foreach(_src
            logos_provider_object.cpp logos_provider_object.h
            qt_provider_object.cpp qt_provider_object.h
            logos_registry_factory.cpp logos_registry_factory.h)
        if(EXISTS "${LOGOS_CPP_SDK_ROOT}/cpp/${_src}")
            list(APPEND PLUGIN_SOURCES "${LOGOS_CPP_SDK_ROOT}/cpp/${_src}")
        endif()
    endforeach()
endif()

set_source_files_properties("${GENERATED_DIR}/logos_sdk.cpp" PROPERTIES GENERATED TRUE)

add_library(dfos_module_plugin SHARED ${PLUGIN_SOURCES})

set_target_properties(dfos_module_plugin PROPERTIES
    PREFIX ""
    OUTPUT_NAME "dfos_module_plugin"
    LIBRARY_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/modules"
    RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/modules"
    BUILD_WITH_INSTALL_RPATH TRUE
    SKIP_BUILD_RPATH FALSE
)

# ── Include dirs ──────────────────────────────────────────────────────────────

target_include_directories(dfos_module_plugin PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/src"
    "${CMAKE_CURRENT_SOURCE_DIR}"
    "${CMAKE_CURRENT_BINARY_DIR}"
    "${GENERATED_DIR}"
    "${DFOS_LIB_DIR}"         # for lib/dfos.h
    "${_interface_dir}"       # for interface.h
)

if(_cpp_sdk_is_source)
    target_include_directories(dfos_module_plugin PRIVATE
        "${LOGOS_CPP_SDK_ROOT}/cpp"
        "${LOGOS_CPP_SDK_ROOT}/cpp/generated"
    )
else()
    target_include_directories(dfos_module_plugin PRIVATE
        "${LOGOS_CPP_SDK_ROOT}/include"
        "${LOGOS_CPP_SDK_ROOT}/include/cpp"
        "${LOGOS_CPP_SDK_ROOT}/include/core"
        "${GENERATED_DIR}/include"
    )
    find_library(LOGOS_SDK_LIB logos_sdk PATHS "${LOGOS_CPP_SDK_ROOT}/lib" NO_DEFAULT_PATH REQUIRED)
endif()

# ── Link ─────────────────────────────────────────────────────────────────────

# Resolve symlink so the linker gets the real path.
get_filename_component(DFOS_LIB_REAL "${DFOS_LIB_PATH}" REALPATH)

add_library(dfos_imported SHARED IMPORTED)
set_target_properties(dfos_imported PROPERTIES IMPORTED_LOCATION "${DFOS_LIB_REAL}")

target_link_libraries(dfos_module_plugin PRIVATE
    Qt${QT_VERSION_MAJOR}::Core
    Qt${QT_VERSION_MAJOR}::RemoteObjects
    dfos_imported
)

if(DEFINED LOGOS_SDK_LIB)
    target_link_libraries(dfos_module_plugin PRIVATE "${LOGOS_SDK_LIB}")
endif()

# ── RPATH (Linux) ─────────────────────────────────────────────────────────────

set_target_properties(dfos_module_plugin PROPERTIES
    INSTALL_RPATH "$ORIGIN"
    INSTALL_RPATH_USE_LINK_PATH FALSE
)

# Copy dfos.so next to the plugin so $ORIGIN resolves correctly.
# If patchelf is available, set a SONAME so the NEEDED entry is just "dfos.so".
find_program(PATCHELF_EXECUTABLE patchelf)

add_custom_command(TARGET dfos_module_plugin POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
        "${DFOS_LIB_REAL}"
        "${CMAKE_BINARY_DIR}/modules/dfos.so"
    COMMENT "Copying dfos.so to modules directory"
)

if(PATCHELF_EXECUTABLE)
    add_custom_command(TARGET dfos_module_plugin POST_BUILD
        COMMAND "${PATCHELF_EXECUTABLE}" --set-soname dfos.so
                "${CMAKE_BINARY_DIR}/modules/dfos.so"
        COMMAND "${PATCHELF_EXECUTABLE}" --replace-needed
                "${DFOS_LIB_REAL}" dfos.so
                "${CMAKE_BINARY_DIR}/modules/dfos_module_plugin.so"
        COMMENT "Patching SONAME and NEEDED entries for dfos.so"
    )
endif()

# Also copy metadata.json
add_custom_command(TARGET dfos_module_plugin POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
        "${METADATA_JSON}"
        "${CMAKE_BINARY_DIR}/modules/metadata.json"
    COMMENT "Copying metadata.json to modules directory"
)
