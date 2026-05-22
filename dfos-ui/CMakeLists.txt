cmake_minimum_required(VERSION 3.16)
project(DfosUiPlugin VERSION 0.1.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)

include(GNUInstallDirs)

# ── Dependency roots ──────────────────────────────────────────────────────────

if(NOT DEFINED LOGOS_CPP_SDK_ROOT)
    if(DEFINED ENV{LOGOS_CPP_SDK_ROOT})
        set(LOGOS_CPP_SDK_ROOT "$ENV{LOGOS_CPP_SDK_ROOT}")
    else()
        set(LOGOS_CPP_SDK_ROOT "${CMAKE_CURRENT_SOURCE_DIR}/vendor/logos-cpp-sdk")
    endif()
endif()

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
    set(_interface_dir "${LOGOS_LIBLOGOS_ROOT}/src/common")
elseif(EXISTS "${LOGOS_LIBLOGOS_ROOT}/interface.h")
    set(_interface_dir "${LOGOS_LIBLOGOS_ROOT}")
elseif(EXISTS "${LOGOS_LIBLOGOS_ROOT}/include/interface.h")
    set(_interface_dir "${LOGOS_LIBLOGOS_ROOT}/include")
else()
    message(FATAL_ERROR "interface.h not found under LOGOS_LIBLOGOS_ROOT=${LOGOS_LIBLOGOS_ROOT}")
endif()

# ── Qt ────────────────────────────────────────────────────────────────────────

find_package(QT NAMES Qt6 Qt5 REQUIRED COMPONENTS Core RemoteObjects Quick QuickWidgets Widgets)
if(Qt6_FOUND)
    set(QT_VERSION_MAJOR 6)
else()
    set(QT_VERSION_MAJOR 5)
endif()
find_package(Qt${QT_VERSION_MAJOR} REQUIRED COMPONENTS Core RemoteObjects Quick QuickWidgets Widgets)

# ── logos-cpp-generator (for logos_sdk.cpp / core_manager_api) ────────────────

set(METADATA_JSON "${CMAKE_CURRENT_SOURCE_DIR}/metadata.json")
set(GENERATED_DIR "${CMAKE_BINARY_DIR}/generated_code")
file(MAKE_DIRECTORY "${GENERATED_DIR}")

get_filename_component(LOGOS_DEPS_ROOT "${LOGOS_CPP_SDK_ROOT}" DIRECTORY)

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
        COMMENT "Generating logos_sdk.cpp for dfos_ui"
        VERBATIM
    )
else()
    message(FATAL_ERROR "logos-cpp-generator not found. Set CPP_GENERATOR or build it first.")
endif()

# ── Plugin target ─────────────────────────────────────────────────────────────

set(PLUGIN_SOURCES
    src/dfos_ui_plugin.h
    src/dfos_ui_plugin.cpp
    src/dfos_backend.h
    src/dfos_backend.cpp
    src/dfos_ui_resources.qrc
    "${GENERATED_DIR}/logos_sdk.cpp"
)

if(_cpp_sdk_is_source)
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

add_library(dfos_ui SHARED ${PLUGIN_SOURCES})

set_target_properties(dfos_ui PROPERTIES
    PREFIX ""
    OUTPUT_NAME "dfos_ui"
    LIBRARY_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/modules"
    RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/modules"
    BUILD_WITH_INSTALL_RPATH TRUE
    SKIP_BUILD_RPATH FALSE
    INSTALL_RPATH "$ORIGIN"
    INSTALL_RPATH_USE_LINK_PATH FALSE
)

# ── Include dirs ──────────────────────────────────────────────────────────────

target_include_directories(dfos_ui PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/src"
    "${CMAKE_CURRENT_SOURCE_DIR}"
    "${CMAKE_CURRENT_SOURCE_DIR}/interfaces"
    "${CMAKE_CURRENT_BINARY_DIR}"
    "${GENERATED_DIR}"
    "${_interface_dir}"
)

if(_cpp_sdk_is_source)
    target_include_directories(dfos_ui PRIVATE
        "${LOGOS_CPP_SDK_ROOT}/cpp"
        "${LOGOS_CPP_SDK_ROOT}/cpp/generated"
    )
else()
    target_include_directories(dfos_ui PRIVATE
        "${LOGOS_CPP_SDK_ROOT}/include"
        "${LOGOS_CPP_SDK_ROOT}/include/cpp"
        "${LOGOS_CPP_SDK_ROOT}/include/core"
        "${GENERATED_DIR}/include"
    )
    find_library(LOGOS_SDK_LIB logos_sdk PATHS "${LOGOS_CPP_SDK_ROOT}/lib" NO_DEFAULT_PATH REQUIRED)
endif()

# ── Link ─────────────────────────────────────────────────────────────────────

target_link_libraries(dfos_ui PRIVATE
    Qt${QT_VERSION_MAJOR}::Core
    Qt${QT_VERSION_MAJOR}::RemoteObjects
    Qt${QT_VERSION_MAJOR}::Quick
    Qt${QT_VERSION_MAJOR}::QuickWidgets
    Qt${QT_VERSION_MAJOR}::Widgets
)

if(DEFINED LOGOS_SDK_LIB)
    target_link_libraries(dfos_ui PRIVATE "${LOGOS_SDK_LIB}")
endif()

# ── Headless loader test ──────────────────────────────────────────────────────

add_executable(test_load test_load.cpp)

target_include_directories(test_load PRIVATE
    "${CMAKE_CURRENT_SOURCE_DIR}/interfaces"
    "${_interface_dir}"
)
if(_cpp_sdk_is_source)
    target_include_directories(test_load PRIVATE "${LOGOS_CPP_SDK_ROOT}/cpp")
else()
    target_include_directories(test_load PRIVATE "${LOGOS_CPP_SDK_ROOT}/include/cpp")
endif()

target_link_libraries(test_load PRIVATE
    Qt${QT_VERSION_MAJOR}::Core
    Qt${QT_VERSION_MAJOR}::Widgets
)

# Also copy metadata.json
add_custom_command(TARGET dfos_ui POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
        "${METADATA_JSON}"
        "${CMAKE_BINARY_DIR}/modules/metadata.json"
    COMMENT "Copying metadata.json to modules directory"
)
